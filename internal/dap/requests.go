package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/debug"
)

func (s *Server) handleInitialize(
	request *protocol.InitializeRequest,
	arguments initializeClientOptions,
) error {
	options, err := normalizeClientOptions(arguments)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	s.stateMu.Lock()
	if s.initialized {
		s.stateMu.Unlock()

		return s.sendFailure(request.GetRequest(), "initialize may only be sent once")
	}

	s.client = options
	s.initialized = true
	s.stateMu.Unlock()

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.InitializeResponse{
			Response: response,
			Body: protocol.Capabilities{
				SupportsConfigurationDoneRequest: true,
				SupportsEvaluateForHovers:        true,
				SupportsTerminateRequest:         true,
			},
		}
	})
}

func (s *Server) handleLaunch(ctx context.Context, request *protocol.LaunchRequest) error {
	s.stateMu.Lock()
	if !s.initialized || s.launched {
		s.stateMu.Unlock()

		return s.sendFailure(request.GetRequest(), "launch requires one successful initialize")
	}
	s.stateMu.Unlock()

	var arguments launchArguments
	decoder := json.NewDecoder(strings.NewReader(string(request.Arguments)))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&arguments); err != nil {
		return s.sendFailure(request.GetRequest(), fmt.Sprintf("invalid launch arguments: %v", err))
	}

	root, program, relativePath, err := resolveLaunchPaths(arguments)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	opened, err := s.workspaces.Open(ctx, root)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	session, err := s.executions.CreateSession(ctx, opened.ID(), relativePath)
	if err != nil {
		_ = s.workspaces.Close(context.Background(), opened.ID())

		return s.sendFailure(request.GetRequest(), err.Error())
	}

	debugSession, err := s.debugs.CreateSession(
		ctx,
		session.ID,
		arguments.Parameters,
		debug.SessionOptions{},
	)
	if err != nil {
		_ = s.executions.CloseSession(context.Background(), session.ID)
		_ = s.workspaces.Close(context.Background(), opened.ID())

		return s.sendFailure(request.GetRequest(), err.Error())
	}

	watch, err := s.debugs.WatchSession(ctx, debugSession.ID)
	if err != nil {
		_ = s.debugs.CloseSession(context.Background(), debugSession.ID)
		_ = s.executions.CloseSession(context.Background(), session.ID)
		_ = s.workspaces.Close(context.Background(), opened.ID())

		return s.sendFailure(request.GetRequest(), err.Error())
	}

	s.stateMu.Lock()
	s.owned = ownedSession{
		workspace:   opened.ID(),
		session:     session.ID,
		debug:       debugSession.ID,
		program:     program,
		stopOnEntry: arguments.StopOnEntry,
	}
	s.watch = watch
	s.launched = true
	s.pendingLaunch = request.GetRequest()
	s.suppressEntry = !arguments.StopOnEntry
	s.stateMu.Unlock()

	if err := s.send(func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.InitializedEvent{Event: protocol.Event{ProtocolMessage: base, Event: "initialized"}}
	}); err != nil {
		return err
	}

	go s.watchDebugSession(watch)

	return nil
}

func (s *Server) resolvePendingLaunch() error {
	request := s.takePendingLaunch()
	if request == nil {
		return nil
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request)
		response.ProtocolMessage = base

		return &protocol.LaunchResponse{Response: response}
	})
}

func (s *Server) failPendingLaunch(message string) error {
	request := s.takePendingLaunch()
	if request == nil {
		return nil
	}

	return s.sendFailure(request, message)
}

func (s *Server) takePendingLaunch() *protocol.Request {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	request := s.pendingLaunch
	s.pendingLaunch = nil

	return request
}

func (s *Server) handleConfigurationDone(ctx context.Context, request *protocol.ConfigurationDoneRequest) error {
	s.stateMu.Lock()
	if !s.launched || s.configured {
		s.stateMu.Unlock()

		return s.sendFailure(request.GetRequest(), "configurationDone requires one successful launch")
	}
	debugID := s.owned.debug
	s.stateMu.Unlock()

	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if err := s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.ConfigurationDoneResponse{Response: response}
	}); err != nil {
		return err
	}

	if _, err := s.debugs.StartSession(ctx, debugID); err != nil {
		result := s.failPendingLaunch(err.Error())

		s.stateMu.Lock()
		s.disconnected = true
		s.stateMu.Unlock()

		return errors.Join(result, s.cleanup())
	}

	s.handles.Reset()
	s.stateMu.Lock()
	s.configured = true
	s.stateMu.Unlock()

	return s.resolvePendingLaunch()
}

func (s *Server) handleSetBreakpoints(ctx context.Context, request *protocol.SetBreakpointsRequest) error {
	debugID, ok := s.configuredDebug(false)
	if !ok {
		return s.sendFailure(request.GetRequest(), "setBreakpoints requires launch before configurationDone")
	}

	if request.Arguments.Source.SourceReference != 0 {
		return s.sendFailure(request.GetRequest(), "source references are not supported")
	}

	path, err := s.sourcePath(request.Arguments.Source.Path)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	if filepath.Clean(path) != filepath.Clean(s.owned.program) {
		return s.sendFailure(request.GetRequest(), "breakpoint source must match the launched program")
	}

	locations := make([]debug.BreakpointLocation, 0, len(request.Arguments.Breakpoints))
	for _, breakpoint := range request.Arguments.Breakpoints {
		if breakpoint.Condition != "" || breakpoint.HitCondition != "" || breakpoint.LogMessage != "" {
			return s.sendFailure(request.GetRequest(), "conditional, hit-count, and log breakpoints are not supported")
		}

		line := s.fromClientLine(breakpoint.Line)
		column := s.fromClientColumn(breakpoint.Column)

		if line < 1 || column < 0 {
			return s.sendFailure(request.GetRequest(), "breakpoint line or column is invalid")
		}

		locations = append(locations, debug.BreakpointLocation{Line: line, Column: column})
	}

	breakpoints, err := s.debugs.ReplaceBreakpoints(ctx, debugID, path, locations)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	clientPath, err := s.clientPath(path)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	result := make([]protocol.Breakpoint, len(breakpoints))
	s.clearNativeBreakpoints(path)

	for index, breakpoint := range breakpoints {
		stableID := s.bindBreakpointID(path, locations[index], breakpoint.ID)
		result[index] = protocol.Breakpoint{
			Id:       stableID,
			Verified: breakpoint.Verified,
			Source:   &protocol.Source{Name: filepath.Base(path), Path: clientPath},
			Line:     s.toClientLine(breakpoint.Line),
			Column:   s.toClientColumn(breakpoint.Column),
		}
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.SetBreakpointsResponse{
			Response: response,
			Body:     protocol.SetBreakpointsResponseBody{Breakpoints: result},
		}
	})
}

func (s *Server) bindBreakpointID(
	file string,
	requested debug.BreakpointLocation,
	nativeID uint64,
) int {
	s.breakpointMu.Lock()
	defer s.breakpointMu.Unlock()

	key := breakpointKey{file: file, line: requested.Line, column: requested.Column}
	stableID := s.stableBreakpoints[key]
	if stableID == 0 {
		stableID = s.nextBreakpointID
		s.nextBreakpointID++
		s.stableBreakpoints[key] = stableID
	}

	s.nativeBreakpoints[nativeID] = stableID
	s.nativeBreakpointFiles[nativeID] = file

	return stableID
}

func (s *Server) clearNativeBreakpoints(file string) {
	s.breakpointMu.Lock()
	defer s.breakpointMu.Unlock()

	for id, existingFile := range s.nativeBreakpointFiles {
		if existingFile == file {
			delete(s.nativeBreakpointFiles, id)
			delete(s.nativeBreakpoints, id)
		}
	}
}

func (s *Server) dapBreakpointID(nativeID uint64) int {
	s.breakpointMu.Lock()
	defer s.breakpointMu.Unlock()

	return s.nativeBreakpoints[nativeID]
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
	debugID, ok := s.configuredDebug(true)
	if !ok || request.Arguments.ThreadId != threadID {
		return s.sendFailure(request.GetRequest(), "pause requires the Ferret thread in a configured session")
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if _, err := s.debugs.PauseSession(ctx, debugID); err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
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
	debugID, ok := s.configuredDebug(true)
	if !ok || requestedThread != threadID {
		return s.sendFailure(request, "request requires the Ferret thread in a configured session")
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if err := resume(debugID); err != nil {
		return s.sendFailure(request, err.Error())
	}
	s.handles.Reset()

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request)
		response.ProtocolMessage = base

		return message(response)
	})
}

func (s *Server) handleThreads(request *protocol.ThreadsRequest) error {
	if _, ok := s.configuredDebug(false); !ok {
		return s.sendFailure(request.GetRequest(), "threads requires a launched session")
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.ThreadsResponse{
			Response: response,
			Body: protocol.ThreadsResponseBody{Threads: []protocol.Thread{{
				Id: threadID, Name: threadName,
			}}},
		}
	})
}

func (s *Server) handleStackTrace(ctx context.Context, request *protocol.StackTraceRequest) error {
	debugID, ok := s.configuredDebug(true)
	if !ok || request.Arguments.ThreadId != threadID {
		return s.sendFailure(request.GetRequest(), "stackTrace requires the Ferret thread in a configured session")
	}

	frames, err := s.debugs.Frames(ctx, debugID)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	start := request.Arguments.StartFrame
	if start < 0 || start > len(frames) {
		return s.sendFailure(request.GetRequest(), "startFrame is invalid")
	}

	end := len(frames)
	if request.Arguments.Levels > 0 && start+request.Arguments.Levels < end {
		end = start + request.Arguments.Levels
	}

	result := make([]protocol.StackFrame, 0, end-start)
	for _, frame := range frames[start:end] {
		path, pathErr := s.clientPath(frame.Location.File)
		if pathErr != nil {
			return s.sendFailure(request.GetRequest(), pathErr.Error())
		}

		result = append(result, protocol.StackFrame{
			Id:     s.handles.Frame(frame.Index),
			Name:   frame.Name,
			Source: &protocol.Source{Name: filepath.Base(frame.Location.File), Path: path},
			Line:   s.toClientLine(frame.Location.Line),
			Column: s.toClientColumn(frame.Location.Column),
		})
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.StackTraceResponse{
			Response: response,
			Body: protocol.StackTraceResponseBody{
				StackFrames: result,
				TotalFrames: len(frames),
			},
		}
	})
}

func (s *Server) handleScopes(ctx context.Context, request *protocol.ScopesRequest) error {
	debugID, ok := s.configuredDebug(true)
	if !ok {
		return s.sendFailure(request.GetRequest(), "scopes requires a configured session")
	}

	frame, ok := s.handles.FrameIndex(request.Arguments.FrameId)
	if !ok {
		return s.sendFailure(request.GetRequest(), "stack frame handle is stale or invalid")
	}

	scopes, err := s.debugs.Scopes(ctx, debugID, frame)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	result := make([]protocol.Scope, len(scopes))
	for index, scope := range scopes {
		result[index] = protocol.Scope{
			Name:               scope.Name,
			PresentationHint:   strings.ToLower(scope.Name),
			VariablesReference: s.handles.Scope(scope.Variables),
			NamedVariables:     len(scope.Variables),
			Expensive:          false,
		}
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.ScopesResponse{
			Response: response,
			Body:     protocol.ScopesResponseBody{Scopes: result},
		}
	})
}

func (s *Server) handleVariables(ctx context.Context, request *protocol.VariablesRequest) error {
	debugID, ok := s.configuredDebug(true)
	if !ok {
		return s.sendFailure(request.GetRequest(), "variables requires a configured session")
	}

	if request.Arguments.Filter != "" || request.Arguments.Start != 0 || request.Arguments.Count != 0 {
		return s.sendFailure(request.GetRequest(), "variable filtering and paging are not supported")
	}

	variables, scope := s.handles.ScopeVariables(request.Arguments.VariablesReference)
	if !scope {
		reference, found := s.handles.VariableReference(request.Arguments.VariablesReference)
		if !found {
			return s.sendFailure(request.GetRequest(), "variable handle is stale or invalid")
		}

		var err error
		variables, err = s.debugs.Variables(ctx, debugID, reference)
		if err != nil {
			return s.sendFailure(request.GetRequest(), err.Error())
		}
	}

	result := make([]protocol.Variable, len(variables))
	for index, variable := range variables {
		result[index] = s.protocolVariable(variable)
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.VariablesResponse{
			Response: response,
			Body:     protocol.VariablesResponseBody{Variables: result},
		}
	})
}

func (s *Server) handleEvaluate(ctx context.Context, request *protocol.EvaluateRequest) error {
	debugID, ok := s.configuredDebug(true)
	if !ok {
		return s.sendFailure(request.GetRequest(), "evaluate requires a configured session")
	}

	frame := 0
	if request.Arguments.FrameId != 0 {
		var found bool
		frame, found = s.handles.FrameIndex(request.Arguments.FrameId)

		if !found {
			return s.sendFailure(request.GetRequest(), "stack frame handle is stale or invalid")
		}
	}

	value, err := s.debugs.Evaluate(ctx, debugID, frame, request.Arguments.Expression)
	if err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
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

func (s *Server) handleTerminate(ctx context.Context, request *protocol.TerminateRequest) error {
	debugID, ok := s.configuredDebug(false)
	if !ok || (request.Arguments != nil && request.Arguments.Restart) {
		return s.sendFailure(request.GetRequest(), "terminate requires a launched session and does not support restart")
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if err := s.failPendingLaunch("launch terminated before configurationDone"); err != nil {
		return err
	}

	if _, err := s.debugs.TerminateSession(ctx, debugID); err != nil {
		return s.sendFailure(request.GetRequest(), err.Error())
	}
	s.handles.Reset()

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.TerminateResponse{Response: response}
	})
}

func (s *Server) handleDisconnect(_ context.Context, request *protocol.DisconnectRequest) error {
	if request.Arguments != nil && (request.Arguments.Restart || request.Arguments.SuspendDebuggee) {
		return s.sendFailure(request.GetRequest(), "disconnect restart and suspend are not supported")
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	if err := s.failPendingLaunch("launch disconnected before configurationDone"); err != nil {
		return err
	}

	if err := s.send(func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.DisconnectResponse{Response: response}
	}); err != nil {
		return err
	}

	s.stateMu.Lock()
	s.disconnected = true
	s.stateMu.Unlock()

	return s.cleanup()
}

func (s *Server) configuredDebug(requireConfigured bool) (debug.SessionID, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if !s.launched || (requireConfigured && !s.configured) || s.disconnected {
		return "", false
	}

	return s.owned.debug, true
}

func (s *Server) protocolVariable(variable debug.Variable) protocol.Variable {
	return protocol.Variable{
		Name:               variable.Name,
		Value:              variable.Value.Display,
		Type:               variable.Value.Type,
		EvaluateName:       variable.Name,
		VariablesReference: s.handles.Variable(variable.Value.Reference),
	}
}
