package dap

import (
	"context"
	"errors"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"
)

func (s *Server) handleCancel(request *protocol.CancelRequest) error {
	s.traceRequest(request.GetRequest())

	return s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.CancelResponse{Response: response}
	})
}

func (s *Server) handleTerminate(_ context.Context, request *protocol.TerminateRequest, targets []*launchAttempt) error {
	restart := request.Arguments != nil && request.Arguments.Restart
	fields := func(event *zerolog.Event) { event.Bool("restart", restart) }
	s.traceRequest(request.GetRequest(), fields)
	s.stateMu.Lock()
	valid := false
	for _, target := range targets {
		if target.admitted {
			valid = true

			break
		}
	}

	launched := s.launched
	s.stateMu.Unlock()

	if !valid || restart {
		return s.sendFailure(request.GetRequest(), errors.New("terminate requires a launched session and does not support restart"), fields)
	}

	cleanupErr := s.closeOwnedLaunch()
	if err := s.failPendingLaunch(errors.Join(errLaunchTerminated, cleanupErr)); err != nil {
		return err
	}

	if err := s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.TerminateResponse{Response: response}
	}); err != nil {
		return err
	}

	if launched {
		s.eventMu.Lock()
		defer s.eventMu.Unlock()

		return s.sendTerminated()
	}

	return nil
}

func (s *Server) handleDisconnect(_ context.Context, request *protocol.DisconnectRequest) error {
	restart := request.Arguments != nil && request.Arguments.Restart
	suspend := request.Arguments != nil && request.Arguments.SuspendDebuggee
	fields := func(event *zerolog.Event) { event.Bool("restart", restart).Bool("suspend_debuggee", suspend) }
	s.traceRequest(request.GetRequest(), fields)

	if restart || suspend {
		return s.sendFailure(request.GetRequest(), errors.New("disconnect restart and suspend are not supported"), fields)
	}

	cleanupErr := s.closeOwnedLaunch()
	if err := s.failPendingLaunch(errors.Join(errLaunchDisconnected, cleanupErr)); err != nil {
		return err
	}

	if err := s.sendResponse(request.GetRequest(), func(base protocol.ProtocolMessage) protocol.Message {
		response := s.response(request.GetRequest())
		response.ProtocolMessage = base

		return &protocol.DisconnectResponse{Response: response}
	}); err != nil {
		return err
	}

	s.stateMu.Lock()
	s.disconnected = true
	s.stateMu.Unlock()

	return nil
}
