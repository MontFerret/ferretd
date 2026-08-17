package dap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Server owns one in-process DAP launch session over a framed stream.
type Server struct {
	reader      *bufio.Reader
	readerClose io.Closer
	writer      io.Writer

	writeMu      sync.Mutex
	eventMu      sync.Mutex
	stateMu      sync.Mutex
	breakpointMu sync.Mutex

	workspaces            *workspace.Manager
	executions            *exec.Manager
	handles               *handleTable
	owned                 ownedSession
	client                clientOptions
	watch                 exec.DebugSubscription
	sequence              int
	initialized           bool
	launched              bool
	configured            bool
	disconnected          bool
	suppressEntry         bool
	nextBreakpointID      int
	stableBreakpoints     map[breakpointKey]int
	nativeBreakpoints     map[uint64]int
	nativeBreakpointFiles map[uint64]string
	cleanupOnce           sync.Once
	cleanupErr            error
}

// New creates a single-session DAP server over input and output.
func New(input io.Reader, output io.Writer) *Server {
	workspaces := workspace.New()
	readerClose, _ := input.(io.Closer)

	return &Server{
		reader:                bufio.NewReader(input),
		readerClose:           readerClose,
		writer:                output,
		workspaces:            workspaces,
		executions:            exec.New(workspaces),
		handles:               newHandleTable(),
		nextBreakpointID:      1,
		stableBreakpoints:     make(map[breakpointKey]int),
		nativeBreakpoints:     make(map[uint64]int),
		nativeBreakpointFiles: make(map[uint64]string),
		client: clientOptions{
			pathFormat:      "path",
			linesStartAt1:   true,
			columnsStartAt1: true,
		},
	}
}

// Run reads and serves DAP requests until disconnect or stream EOF.
func (s *Server) Run(ctx context.Context) (result error) {
	stopCancellation := make(chan struct{})
	if s.readerClose != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = s.readerClose.Close()
			case <-stopCancellation:
			}
		}()
	}

	defer func() {
		close(stopCancellation)
		result = errors.Join(result, s.cleanup())
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		message, err := protocol.ReadProtocolMessage(s.reader)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return contextErr
			}

			if errors.Is(err, io.EOF) {
				return nil
			}

			return fmt.Errorf("read DAP message: %w", err)
		}

		request, ok := message.(protocol.RequestMessage)
		if !ok {
			continue
		}

		if err := s.dispatch(ctx, request); err != nil {
			return err
		}

		s.stateMu.Lock()
		disconnected := s.disconnected
		s.stateMu.Unlock()
		if disconnected {
			return nil
		}
	}
}

func (s *Server) dispatch(ctx context.Context, request protocol.RequestMessage) error {
	switch typed := request.(type) {
	case *protocol.InitializeRequest:
		return s.handleInitialize(typed)
	case *protocol.LaunchRequest:
		return s.handleLaunch(ctx, typed)
	case *protocol.ConfigurationDoneRequest:
		return s.handleConfigurationDone(ctx, typed)
	case *protocol.SetBreakpointsRequest:
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
		return s.handleTerminate(ctx, typed)
	case *protocol.DisconnectRequest:
		return s.handleDisconnect(ctx, typed)
	default:
		return s.sendFailure(request.GetRequest(), "request is not supported")
	}
}

func (s *Server) send(build func(protocol.ProtocolMessage) protocol.Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.sequence++
	message := build(protocol.ProtocolMessage{Seq: s.sequence})

	switch typed := message.(type) {
	case protocol.ResponseMessage:
		typed.GetResponse().Type = "response"
	case protocol.EventMessage:
		typed.GetEvent().Type = "event"
	case protocol.RequestMessage:
		typed.GetRequest().Type = "request"
	}

	if err := protocol.WriteProtocolMessage(s.writer, message); err != nil {
		return fmt.Errorf("write DAP message: %w", err)
	}

	return nil
}

func (s *Server) sendFailure(request *protocol.Request, message string) error {
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
				Format:   message,
				ShowUser: true,
			}},
		}
	})
}

func (s *Server) response(request *protocol.Request) protocol.Response {
	return protocol.Response{
		RequestSeq: request.Seq,
		Success:    true,
		Command:    request.Command,
	}
}

func (s *Server) cleanup() error {
	s.cleanupOnce.Do(func() {
		if s.watch.Cancel != nil {
			s.watch.Cancel()
		}

		ctx := context.Background()
		var result error

		if s.owned.debug != "" {
			_, _ = s.executions.TerminateDebugSession(ctx, s.owned.debug)
			result = errors.Join(result, s.executions.CloseDebugSession(ctx, s.owned.debug))
		}

		if s.owned.session != "" {
			result = errors.Join(result, s.executions.CloseSession(ctx, s.owned.session))
		}

		if s.owned.workspace != "" {
			result = errors.Join(result, s.workspaces.Close(ctx, s.owned.workspace))
		}

		result = errors.Join(result, s.executions.Close(ctx))
		result = errors.Join(result, s.workspaces.Clear(ctx))
		s.cleanupErr = result
	})

	return s.cleanupErr
}
