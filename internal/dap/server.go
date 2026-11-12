// Package dap adapts transport-neutral execution and debug managers to DAP stdio.
package dap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	// Server owns one in-process DAP launch session over a framed stream.
	Server struct {
		reader      *bufio.Reader
		readerClose io.Closer
		writer      io.Writer
		writerClose io.Closer

		// writeMu protects the framed writer and outbound sequence. Successful-send
		// trace records remain inside the same ordering boundary.
		writeMu sync.Mutex
		// eventMu preserves command/event ordering and protects initial-stop
		// bindings, breakpoint IDs, and terminal-event deduplication.
		eventMu sync.Mutex
		// stateMu protects resource handoff, client options, watches, cancellation
		// registration, pending launch, and adapter closing state.
		stateMu sync.Mutex

		workspaces     *workspace.Manager
		executions     *exec.Manager
		debugs         *debug.Manager
		runtimeOwner   *runtimes.Owner
		catalog        *runtimes.Catalog
		logger         zerolog.Logger
		handles        *handleTable
		owned          ownedSession
		client         clientOptions
		watch          debug.Subscription
		pendingLaunch  *launchAttempt
		queuedLaunches map[*launchAttempt]struct{}
		currentLaunch  *launchAttempt
		activeLaunch   *launchAttempt
		activeCancel   context.CancelCauseFunc
		runStop        context.CancelFunc
		closing        bool
		failure        error
		rollbackErr    error
		ownedClosed    bool
		ownedCloseErr  error
		watchStop      chan struct{}
		watchDone      chan struct{}
		terminalSent   bool
		sequence       int
		initialized    bool
		launched       bool
		configured     bool
		disconnected   bool
		initialStop    *initialStop
		breakpoints    *breakpointIDs
		watchFailure   error
		wake           chan struct{}
		cleanupOnce    sync.Once
		cleanupErr     error
	}

	ownedSession struct {
		workspace        workspace.ID
		session          exec.SessionID
		debug            debug.SessionID
		root             string
		program          string
		programIdentity  sourceIdentity
		stopOnEntry      bool
		coordinates      *sourceCoordinates
		runtimeName      string
		workingDirectory string
	}
)

func (s *Server) dispatch(
	ctx context.Context,
	request protocol.RequestMessage,
	initializeOptions initializeClientOptions,
	attempt *launchAttempt,
	targets []*launchAttempt,
) error {
	switch typed := request.(type) {
	case *protocol.InitializeRequest:
		return s.handleInitialize(typed, initializeOptions)
	case *protocol.LaunchRequest:
		return s.handleLaunch(ctx, typed, attempt)
	case *protocol.ConfigurationDoneRequest:
		return s.handleConfigurationDone(ctx, typed)
	case *sourceBreakpointsRequest:
		return s.handleSetBreakpoints(ctx, typed)
	case *protocol.ContinueRequest:
		return s.handleContinue(ctx, typed)
	case *protocol.PauseRequest:
		return s.handlePause(ctx, typed)
	case *protocol.NextRequest:
		return s.handleNext(ctx, typed)
	case *protocol.StepInRequest:
		return s.handleStepIn(ctx, typed)
	case *protocol.StepOutRequest:
		return s.handleStepOut(ctx, typed)
	case *protocol.ThreadsRequest:
		return s.handleThreads(typed)
	case *protocol.StackTraceRequest:
		return s.handleStackTrace(ctx, typed)
	case *protocol.ScopesRequest:
		return s.handleScopes(ctx, typed)
	case *protocol.VariablesRequest:
		return s.handleVariables(ctx, typed)
	case *protocol.EvaluateRequest:
		return s.handleEvaluate(ctx, typed)
	case *protocol.TerminateRequest:
		return s.handleTerminate(ctx, typed, targets)
	case *protocol.CancelRequest:
		return s.handleCancel(typed)
	case *protocol.DisconnectRequest:
		return s.handleDisconnect(ctx, typed)
	default:
		s.traceRequest(request.GetRequest())

		return s.sendFailure(request.GetRequest(), errors.New("request is not supported"))
	}
}

func (s *Server) send(
	build func(protocol.ProtocolMessage) protocol.Message,
	sent func(int),
) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.stateMu.Lock()
	closing := s.closing
	s.stateMu.Unlock()

	if closing {
		return errAdapterClosing
	}

	s.sequence++

	message := build(protocol.ProtocolMessage{Seq: s.sequence})
	if message == nil {
		s.sequence--

		return nil
	}

	switch typed := message.(type) {
	case protocol.ResponseMessage:
		typed.GetResponse().Type = "response"
	case protocol.EventMessage:
		typed.GetEvent().Type = "event"
	case protocol.RequestMessage:
		typed.GetRequest().Type = "request"
	}

	if err := protocol.WriteProtocolMessage(s.writer, message); err != nil {
		failure := fmt.Errorf("write DAP message: %w", err)
		s.stop(failure)

		return failure
	}

	if sent != nil {
		sent(s.sequence)
	}

	return nil
}

func (s *Server) sendFailure(request *protocol.Request, requestErr error, enrichers ...logEnricher) error {
	return s.sendFailureWithDiagnostic(request, requestErr, requestErr, enrichers...)
}

func (s *Server) sendFailureWithDiagnostic(
	request *protocol.Request,
	requestErr error,
	diagnosticErr error,
	enrichers ...logEnricher,
) error {
	return s.sendFailureDetails(request, requestErr, requestErr, diagnosticErr, enrichers...)
}

func (s *Server) sendFailureDetails(
	request *protocol.Request,
	requestErr error,
	details error,
	diagnosticErr error,
	enrichers ...logEnricher,
) error {
	s.logRequestFailure(request, diagnosticErr, enrichers...)
	message := requestErr.Error()

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.ErrorResponse{
			Response: protocol.Response{
				ProtocolMessage: base,
				RequestSeq:      request.Seq,
				Success:         false,
				Command:         request.Command,
				Message:         message,
			},
			Body: protocol.ErrorResponseBody{Error: &protocol.ErrorMessage{
				Id:       1,
				Format:   details.Error(),
				ShowUser: true,
			}},
		}
	}, nil)
}

func (s *Server) response(request *protocol.Request) protocol.Response {
	return protocol.Response{
		RequestSeq: request.Seq,
		Success:    true,
		Command:    request.Command,
	}
}
