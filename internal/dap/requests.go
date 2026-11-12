package dap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/debug"
)

func (s *Server) handleInitialize(
	request *protocol.InitializeRequest,
	arguments initializeClientOptions,
) error {
	s.traceRequest(request.GetRequest(), func(event *zerolog.Event) {
		event.Str("path_format", request.Arguments.PathFormat)
	})

	options, err := arguments.normalized()
	if err != nil {
		return s.sendFailure(request.GetRequest(), err, func(event *zerolog.Event) {
			event.Str("path_format", request.Arguments.PathFormat)
		})
	}

	s.stateMu.Lock()
	if s.initialized {
		s.stateMu.Unlock()

		return s.sendFailure(request.GetRequest(), errors.New("initialize may only be sent once"))
	}

	s.client = options
	s.initialized = true
	s.stateMu.Unlock()

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &initializeResponse{
			Response: response,
			Body: initializeCapabilities{
				SupportsFerretRuntimeSelection: true,
				Capabilities: protocol.Capabilities{
					SupportsCancelRequest:            true,
					SupportsConfigurationDoneRequest: true,
					SupportsEvaluateForHovers:        true,
					SupportsTerminateRequest:         true,
				},
			},
		}
	})
}

func (s *Server) handleConfigurationDone(ctx context.Context, request *protocol.ConfigurationDoneRequest) error {
	s.traceRequest(request.GetRequest())
	s.stateMu.Lock()
	if !s.launched || s.ownedClosed || s.configured {
		s.stateMu.Unlock()

		return s.sendFailure(request.GetRequest(), errors.New("configurationDone requires one successful launch"))
	}

	debugID := s.owned.debug
	s.stateMu.Unlock()
	s.eventMu.Lock()
	if err := s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.ConfigurationDoneResponse{Response: response}
	}); err != nil {
		s.eventMu.Unlock()

		return err
	}

	if _, err := s.debugs.StartSession(ctx, debugID); err != nil {
		s.stateMu.Lock()

		interrupted := s.closing
		if attempt := s.pendingLaunch; attempt != nil && attempt.cause != nil {
			attempt.cause = errors.Join(attempt.cause, err)
			interrupted = true
		}

		s.stateMu.Unlock()
		s.initialStop = nil
		s.eventMu.Unlock()

		if interrupted {
			// Reconciliation and queued controls own cancellation responses and
			// settlement. Ending dispatch here would drop their acknowledgments.
			return nil
		}

		cleanupErr := s.closeOwnedLaunch()
		result := s.failPendingLaunch(errors.Join(err, cleanupErr))
		s.stateMu.Lock()
		s.disconnected = true
		s.stateMu.Unlock()

		return result
	}

	s.initialStop.frozen = true
	s.breakpoints.commandStarted()
	s.invalidateHandles("configurationDone")
	s.stateMu.Lock()
	s.configured = true
	s.stateMu.Unlock()
	s.logger.Info().Msg("DAP configuration completed")
	result := s.resolvePendingLaunch()
	s.eventMu.Unlock()

	return result
}

func (s *Server) handleSetBreakpoints(ctx context.Context, request *sourceBreakpointsRequest) error {
	breakpointFields := func(event *zerolog.Event) {
		event.
			Str("source", request.Arguments.Source.Path).
			Int("count", len(request.Arguments.Breakpoints))
	}
	s.traceRequest(request.GetRequest(), breakpointFields)

	debugID, ok := s.configuredDebug(false)
	if !ok {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("setBreakpoints requires launch before configurationDone"),
			breakpointFields,
		)
	}

	if request.Arguments.Source.SourceReference != 0 {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("source references are not supported"),
			func(event *zerolog.Event) {
				breakpointFields(event)
				event.Int("source_reference", request.Arguments.Source.SourceReference)
			},
		)
	}

	path, err := s.sourcePath(request.Arguments.Source.Path)
	if err != nil {
		return s.sendFailure(
			request.GetRequest(),
			err,
			breakpointFields,
		)
	}

	identity, err := newSourceIdentity(path, s.owned.root)
	if err != nil {
		sourcePath := identity.path
		if sourcePath == "" {
			sourcePath = path
		}

		s.logger.Warn().
			Err(err).
			Str("source", sourcePath).
			Str("launch_program", s.owned.program).
			Str("launch_program_canonical", s.owned.programIdentity.canonical).
			Int("count", len(request.Arguments.Breakpoints)).
			Msg("DAP breakpoint source is unavailable")

		return s.sendUnverifiedBreakpoints(request, sourcePath, "breakpoint source is unavailable")
	}

	if !identity.same(s.owned.programIdentity) {
		s.logger.Warn().
			Str("source", identity.path).
			Str("source_canonical", identity.canonical).
			Str("launch_program", s.owned.program).
			Str("launch_program_canonical", s.owned.programIdentity.canonical).
			Int("count", len(request.Arguments.Breakpoints)).
			Msg("DAP breakpoint source does not match launched program")

		return s.sendUnverifiedBreakpoints(
			request,
			identity.path,
			"breakpoints are only supported for the launched program",
		)
	}

	requests := make([]apidebugger.BreakpointRequest, 0, len(request.Arguments.Breakpoints))
	for index, breakpoint := range request.Arguments.Breakpoints {
		if breakpoint.Condition != "" || breakpoint.HitCondition != "" || breakpoint.LogMessage != "" {
			return s.sendFailure(
				request.GetRequest(),
				errors.New("conditional, hit-count, and log breakpoints are not supported"),
				func(event *zerolog.Event) {
					event.Str("source", path).Int("count", len(request.Arguments.Breakpoints))
				},
			)
		}

		column, err := request.column(index)
		if err != nil {
			return s.sendFailure(request.GetRequest(), err, breakpointFields)
		}

		position, err := s.owned.coordinates.fromClient(breakpoint.Line, column)
		if err != nil {
			return s.sendFailure(
				request.GetRequest(),
				err,
				func(event *zerolog.Event) {
					event.
						Str("source", path).
						Int("line", breakpoint.Line).
						Int("column", breakpoint.Column)
				},
			)
		}

		requests = append(requests, apidebugger.BreakpointRequest{
			Position: position,
			Options:  apidebugger.BreakpointOptions{BindingMode: apidebugger.BreakpointBindNextExecutableInSource},
		})
	}

	clientPath, err := s.clientPath(identity.path)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err, func(event *zerolog.Event) {
			event.Str("source", path).Int("count", len(requests))
		})
	}

	// Publish ID mappings and the response before any stop from this replacement.
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	breakpoints, err := s.debugs.ReplaceBreakpoints(ctx, debugID, s.owned.program, requests)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err, func(event *zerolog.Event) {
			event.Str("source", path).Int("count", len(requests))
		})
	}

	s.breakpoints.replace(s.owned.program, requests, breakpoints)

	if s.initialStop != nil {
		s.initialStop.replace(breakpoints)
	}

	result := make([]sourceBreakpoint, len(breakpoints))

	for index, breakpoint := range breakpoints {
		location := breakpoint.RequestedLocation
		if breakpoint.Bound {
			location = breakpoint.Location.Location
		}

		position, err := s.owned.coordinates.toClient(location.Position)
		if err != nil {
			return s.sendFailure(request.GetRequest(), fmt.Errorf("convert native breakpoint position: %w", err), breakpointFields)
		}

		if breakpoint.Bound && position.column == nil {
			return s.sendFailure(request.GetRequest(), errors.New("native bound breakpoint position has no column"), breakpointFields)
		}

		result[index] = sourceBreakpoint{
			Breakpoint: protocol.Breakpoint{
				Id:       s.breakpoints.id(breakpoint.ID),
				Verified: breakpoint.Bound,
				Source:   &protocol.Source{Name: filepath.Base(identity.path), Path: clientPath},
			},
			Line:   &position.line,
			Column: position.column,
		}
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		message := &sourceBreakpointsResponse{Response: response}
		message.Body.Breakpoints = result

		return message
	}, func(event *zerolog.Event) {
		event.Int("count", len(result))
	})
}

func (s *Server) sendUnverifiedBreakpoints(
	request *sourceBreakpointsRequest,
	path string,
	message string,
) error {
	sourceValue := request.Arguments.Source

	if clientPath, err := s.clientPath(path); err == nil {
		sourceValue.Path = clientPath
	}

	if sourceValue.Name == "" {
		sourceValue.Name = filepath.Base(path)
	}

	result := make([]sourceBreakpoint, len(request.Arguments.Breakpoints))
	for index, breakpoint := range request.Arguments.Breakpoints {
		column, err := request.column(index)
		if err != nil {
			return s.sendFailure(request.GetRequest(), err)
		}

		result[index] = sourceBreakpoint{
			Breakpoint: protocol.Breakpoint{
				Verified: false,
				Message:  message,
				Source:   &sourceValue,
			},
			Line:   &breakpoint.Line,
			Column: column,
		}
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		message := &sourceBreakpointsResponse{Response: response}
		message.Body.Breakpoints = result

		return message
	}, func(event *zerolog.Event) {
		event.Int("count", len(result))
	})
}

func (s *Server) handleContinue(ctx context.Context, request *protocol.ContinueRequest) error {
	return s.handleResume(request.GetRequest(), request.Arguments.ThreadId, func(id debug.SessionID) error {
		_, err := s.debugs.ContinueSession(ctx, id)

		return err
	}, func(response protocol.Response) protocol.Message {
		return &protocol.ContinueResponse{
			Response: response,
			Body:     protocol.ContinueResponseBody{AllThreadsContinued: true},
		}
	})
}

func (s *Server) handlePause(ctx context.Context, request *protocol.PauseRequest) error {
	threadFields := func(event *zerolog.Event) {
		event.Int("thread_id", request.Arguments.ThreadId)
	}
	s.traceRequest(request.GetRequest(), threadFields)

	debugID, ok := s.configuredDebug(true)
	if !ok || request.Arguments.ThreadId != threadID {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("pause requires the Ferret thread in a configured session"),
			threadFields,
		)
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if _, err := s.debugs.PauseSession(ctx, debugID); err != nil {
		return s.sendFailure(request.GetRequest(), err, threadFields)
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.PauseResponse{Response: response}
	})
}

func (s *Server) handleNext(ctx context.Context, request *protocol.NextRequest) error {
	return s.handleResume(request.GetRequest(), request.Arguments.ThreadId, func(id debug.SessionID) error {
		_, err := s.debugs.StepOverSession(ctx, id)

		return err
	}, func(response protocol.Response) protocol.Message {
		return &protocol.NextResponse{Response: response}
	})
}

func (s *Server) handleStepIn(ctx context.Context, request *protocol.StepInRequest) error {
	return s.handleResume(request.GetRequest(), request.Arguments.ThreadId, func(id debug.SessionID) error {
		_, err := s.debugs.StepInSession(ctx, id)

		return err
	}, func(response protocol.Response) protocol.Message {
		return &protocol.StepInResponse{Response: response}
	})
}

func (s *Server) handleStepOut(ctx context.Context, request *protocol.StepOutRequest) error {
	return s.handleResume(request.GetRequest(), request.Arguments.ThreadId, func(id debug.SessionID) error {
		_, err := s.debugs.StepOutSession(ctx, id)

		return err
	}, func(response protocol.Response) protocol.Message {
		return &protocol.StepOutResponse{Response: response}
	})
}

func (s *Server) handleResume(
	request *protocol.Request,
	requestedThread int,
	resume func(debug.SessionID) error,
	message func(protocol.Response) protocol.Message,
) error {
	threadFields := func(event *zerolog.Event) {
		event.Int("thread_id", requestedThread)
	}
	s.traceRequest(request, threadFields)

	debugID, ok := s.configuredDebug(true)
	if !ok || requestedThread != threadID {
		return s.sendFailure(
			request,
			errors.New("request requires the Ferret thread in a configured session"),
			threadFields,
		)
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if err := resume(debugID); err != nil {
		return s.sendFailure(request, err, threadFields)
	}

	s.breakpoints.commandStarted()
	s.invalidateHandles(request.Command)

	return s.sendResponse(request, func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request)
		response.ProtocolMessage = base

		return message(response)
	}, threadFields)
}

func (s *Server) handleThreads(request *protocol.ThreadsRequest) error {
	s.traceRequest(request.GetRequest())

	if _, ok := s.configuredDebug(false); !ok {
		return s.sendFailure(request.GetRequest(), errors.New("threads requires a launched session"))
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.ThreadsResponse{
			Response: response,
			Body: protocol.ThreadsResponseBody{Threads: []protocol.Thread{{
				Id: threadID, Name: threadName,
			}}},
		}
	}, func(event *zerolog.Event) {
		event.Int("count", 1)
	})
}

func (s *Server) handleStackTrace(ctx context.Context, request *protocol.StackTraceRequest) error {
	stackFields := func(event *zerolog.Event) {
		event.
			Int("thread_id", request.Arguments.ThreadId).
			Int("start_frame", request.Arguments.StartFrame).
			Int("levels", request.Arguments.Levels)
	}
	s.traceRequest(request.GetRequest(), stackFields)

	debugID, ok := s.configuredDebug(true)
	if !ok || request.Arguments.ThreadId != threadID {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("stackTrace requires the Ferret thread in a configured session"),
			stackFields,
		)
	}

	frames, err := s.debugs.Frames(ctx, debugID)
	if err != nil {
		return s.sendFailure(
			request.GetRequest(),
			err,
			stackFields,
		)
	}

	start := request.Arguments.StartFrame
	if start < 0 || start > len(frames) {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("startFrame is invalid"),
			func(event *zerolog.Event) {
				event.
					Int("thread_id", request.Arguments.ThreadId).
					Int("start_frame", start).
					Int("levels", request.Arguments.Levels)
			},
		)
	}

	end := len(frames)
	if request.Arguments.Levels > 0 && start+request.Arguments.Levels < end {
		end = start + request.Arguments.Levels
	}

	result := make([]protocol.StackFrame, 0, end-start)
	for offset, frame := range frames[start:end] {
		path, pathErr := s.clientPath(frame.Location.SourceName)
		if pathErr != nil {
			return s.sendFailure(
				request.GetRequest(),
				pathErr,
				func(event *zerolog.Event) {
					event.
						Int("thread_id", request.Arguments.ThreadId).
						Str("source", frame.Location.SourceName)
				},
			)
		}

		position, err := s.owned.coordinates.toClient(frame.Location.Position)
		if err != nil {
			return s.sendFailure(request.GetRequest(), fmt.Errorf("convert native frame position: %w", err), stackFields)
		}

		if position.column == nil {
			return s.sendFailure(request.GetRequest(), errors.New("native frame position has no column"), stackFields)
		}

		// Universal frame indices address positions in the complete current Frames() result.
		frameIndex := start + offset
		frameID := s.handles.Frame(frameIndex)
		s.logger.Debug().
			Int("frame_id", frameID).
			Int("frame_index", frameIndex).
			Msg("DAP stack frame handle allocated")
		result = append(result, protocol.StackFrame{
			Id:     frameID,
			Name:   frame.Name,
			Source: &protocol.Source{Name: filepath.Base(frame.Location.SourceName), Path: path},
			Line:   position.line,
			Column: *position.column,
		})
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.StackTraceResponse{
			Response: response,
			Body: protocol.StackTraceResponseBody{
				StackFrames: result,
				TotalFrames: len(frames),
			},
		}
	}, func(event *zerolog.Event) {
		event.Int("frames", len(result)).Int("total_frames", len(frames))
	})
}

func (s *Server) handleScopes(ctx context.Context, request *protocol.ScopesRequest) error {
	frameFields := func(event *zerolog.Event) {
		event.Int("frame_id", request.Arguments.FrameId)
	}
	s.traceRequest(request.GetRequest(), frameFields)

	debugID, ok := s.configuredDebug(true)
	if !ok {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("scopes requires a configured session"),
			frameFields,
		)
	}

	frame, status := s.handles.FrameIndex(request.Arguments.FrameId)
	if status == handleInvalid {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("stack frame handle is stale or invalid"),
			frameFields,
		)
	}

	stale := status == handleStale
	result := make([]protocol.Scope, 0)

	if !stale {
		scopes, err := s.debugs.Scopes(ctx, debugID, frame)
		if err != nil {
			return s.sendFailure(
				request.GetRequest(),
				err,
				func(event *zerolog.Event) {
					frameFields(event)
					event.Int("frame_index", frame)
				},
			)
		}

		result = make([]protocol.Scope, len(scopes))
		for index, scope := range scopes {
			result[index] = protocol.Scope{
				Name:               scope.Name,
				PresentationHint:   strings.ToLower(scope.Name),
				VariablesReference: s.handles.Scope(scope.Variables),
				NamedVariables:     len(scope.Variables),
				Expensive:          false,
			}
		}
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.ScopesResponse{
			Response: response,
			Body:     protocol.ScopesResponseBody{Scopes: result},
		}
	}, func(event *zerolog.Event) {
		frameFields(event)
		event.Int("scopes", len(result))

		if stale {
			event.Bool("stale", true)
		}
	})
}

func (s *Server) handleVariables(ctx context.Context, request *protocol.VariablesRequest) error {
	variableFields := func(event *zerolog.Event) {
		event.Int("variables_reference", request.Arguments.VariablesReference)
	}
	s.traceRequest(request.GetRequest(), variableFields)

	debugID, ok := s.configuredDebug(true)
	if !ok {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("variables requires a configured session"),
			variableFields,
		)
	}

	if request.Arguments.Filter != "" || request.Arguments.Start != 0 || request.Arguments.Count != 0 {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("variable filtering and paging are not supported"),
			func(event *zerolog.Event) {
				variableFields(event)
				event.Int("start", request.Arguments.Start).Int("count", request.Arguments.Count)
			},
		)
	}

	variables, status := s.handles.ScopeVariables(request.Arguments.VariablesReference)

	stale := status == handleStale
	if status == handleInvalid {
		reference, referenceStatus := s.handles.VariableReference(request.Arguments.VariablesReference)
		switch referenceStatus {
		case handleCurrent:
			var err error

			variables, err = s.debugs.Variables(ctx, debugID, reference)
			if err != nil {
				return s.sendFailure(
					request.GetRequest(),
					err,
					variableFields,
				)
			}
		case handleStale:
			stale = true
		default:
			return s.sendFailure(
				request.GetRequest(),
				errors.New("variable handle is stale or invalid"),
				variableFields,
			)
		}
	}

	result := make([]protocol.Variable, len(variables))
	for index, variable := range variables {
		result[index] = s.protocolVariable(variable)
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.VariablesResponse{
			Response: response,
			Body:     protocol.VariablesResponseBody{Variables: result},
		}
	}, func(event *zerolog.Event) {
		variableFields(event)
		event.Int("variables", len(result))

		if stale {
			event.Bool("stale", true)
		}
	})
}

func (s *Server) handleEvaluate(ctx context.Context, request *protocol.EvaluateRequest) error {
	evaluateFields := func(event *zerolog.Event) {
		event.
			Str("context", request.Arguments.Context).
			Int("frame_id", request.Arguments.FrameId).
			Int("expression_length", len(request.Arguments.Expression))
	}
	s.traceRequest(request.GetRequest(), evaluateFields)

	debugID, ok := s.configuredDebug(true)
	if !ok {
		return s.sendFailure(
			request.GetRequest(),
			errors.New("evaluate requires a configured session"),
			evaluateFields,
		)
	}

	if strings.TrimSpace(request.Arguments.Expression) == "" {
		return s.sendEmptyEvaluateResponse(request.GetRequest(), evaluateFields, true, false)
	}

	frame := 0

	if request.Arguments.FrameId != 0 {
		var status handleStatus

		frame, status = s.handles.FrameIndex(request.Arguments.FrameId)
		if status == handleStale &&
			(request.Arguments.Context == "hover" || request.Arguments.Context == "watch") {
			return s.sendEmptyEvaluateResponse(request.GetRequest(), evaluateFields, false, true)
		}

		if status != handleCurrent {
			return s.sendFailure(
				request.GetRequest(),
				errors.New("stack frame handle is stale or invalid"),
				evaluateFields,
			)
		}
	}

	value, err := s.debugs.Evaluate(ctx, debugID, frame, request.Arguments.Expression)
	if err != nil {
		diagnosticErr := err
		diagnosticFields := logEnricher(evaluateFields)

		if strings.TrimSpace(request.Arguments.Expression) != "" {
			diagnosticErr = errors.New("debug evaluation failed")
			diagnosticFields = func(event *zerolog.Event) {
				evaluateFields(event)
				event.Str("error_type", fmt.Sprintf("%T", err))
			}
		}

		return s.sendFailureWithDiagnostic(request.GetRequest(), err, diagnosticErr, diagnosticFields)
	}

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.EvaluateResponse{
			Response: response,
			Body: protocol.EvaluateResponseBody{
				Result:             value.Display,
				Type:               value.Type,
				VariablesReference: s.handles.Variable(value.Reference),
			},
		}
	})
}

func (s *Server) sendEmptyEvaluateResponse(
	request *protocol.Request,
	evaluateFields logEnricher,
	empty bool,
	stale bool,
) error {
	return s.sendResponse(request, func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request)
		response.ProtocolMessage = base

		return &protocol.EvaluateResponse{
			Response: response,
			Body: protocol.EvaluateResponseBody{
				Result:             "",
				VariablesReference: 0,
			},
		}
	}, func(event *zerolog.Event) {
		evaluateFields(event)

		if empty {
			event.Bool("empty", true)
		}

		if stale {
			event.Bool("stale", true)
		}
	})
}

func (s *Server) configuredDebug(requireConfigured bool) (debug.SessionID, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if !s.launched || (requireConfigured && !s.configured) || s.disconnected {
		return "", false
	}

	return s.owned.debug, true
}

func (s *Server) protocolVariable(variable apidebugger.Variable) protocol.Variable {
	return protocol.Variable{
		Name:               variable.Name,
		Value:              variable.Value.Display,
		Type:               variable.Value.Type,
		EvaluateName:       variable.Name,
		VariablesReference: s.handles.Variable(variable.Value.Reference),
	}
}
