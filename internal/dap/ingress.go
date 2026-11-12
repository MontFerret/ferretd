package dap

import (
	"context"
	"errors"
	"fmt"
	"io"

	protocol "github.com/google/go-dap"
)

const requestQueueCapacity = 64

type (
	requestEnvelope struct {
		request protocol.RequestMessage
		options initializeClientOptions
		launch  *launchAttempt
		targets []*launchAttempt
	}

	// launchAttempt survives its preparation request and configuration period.
	// All mutable fields belong to Server.stateMu; it owns no debugger lifetime.
	launchAttempt struct {
		// Copy the base fields so retaining the attempt cannot pin launch arguments.
		request     protocol.Request
		cause       error
		admitted    bool
		runtimeName string
	}
)

// Run reads framed ingress independently of its one serialized dispatch worker.
// Cancellation can interrupt an active runtime call without waiting for eventMu.
// As with stdio, blocking streams must implement io.Closer to interrupt I/O.
func (s *Server) Run(ctx context.Context) (result error) {
	if ctx == nil {
		return errors.Join(errors.New("DAP: nil context"), s.cleanup())
	}

	s.logger.Info().Msg("DAP session started")
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	s.stateMu.Lock()
	s.runStop = cancel
	s.stateMu.Unlock()

	queue := make(chan requestEnvelope, requestQueueCapacity)
	dispatched := make(chan struct{})
	interrupted := make(chan struct{})
	go func() {
		defer close(interrupted)
		select {
		case <-ctx.Done():
		case <-runCtx.Done():
		}

		s.stop(ctx.Err())

		if s.readerClose != nil {
			_ = s.readerClose.Close()
		}

		if s.writerClose != nil {
			_ = s.writerClose.Close()
		}
	}()
	go func() {
		defer close(dispatched)
		err := s.serveRequests(runCtx, queue)
		s.stop(err)
	}()

	defer func() {
		s.stop(nil)
		close(queue)
		<-dispatched
		<-interrupted
		s.stateMu.Lock()
		result = errors.Join(s.failure, ctx.Err())
		s.runStop = nil
		s.stateMu.Unlock()
		result = errors.Join(result, s.cleanup())
		status := "completed"

		if errors.Is(result, context.Canceled) {
			status = "canceled"
		} else if result != nil {
			status = "failed"
		}

		s.logger.Info().Str("status", status).Msg("DAP session ended")
	}()

	for runCtx.Err() == nil {
		message, options, err := readProtocolMessage(s.reader)
		if err != nil {
			if runCtx.Err() == nil && !errors.Is(err, io.EOF) {
				s.stop(fmt.Errorf("read DAP message: %w", err))
			}

			return nil
		}

		request, ok := message.(protocol.RequestMessage)
		if !ok {
			continue
		}

		envelope := s.receiveRequest(request, options)
		select {
		case queue <- envelope:
		default:
			s.stop(errRequestOverload)

			return nil
		}

		if disconnect, ok := request.(*protocol.DisconnectRequest); ok &&
			(disconnect.Arguments == nil || (!disconnect.Arguments.Restart && !disconnect.Arguments.SuspendDebuggee)) {
			// No further ingress is needed, but let the worker acknowledge and
			// settle the disconnect before stopping the transport.
			<-dispatched

			return nil
		}
	}

	return nil
}

func (s *Server) receiveRequest(request protocol.RequestMessage, options initializeClientOptions) requestEnvelope {
	envelope := requestEnvelope{request: request, options: options}
	s.stateMu.Lock()

	if _, ok := request.(*protocol.LaunchRequest); ok {
		envelope.launch = &launchAttempt{request: *request.GetRequest()}
		s.queuedLaunches[envelope.launch] = struct{}{}
	}

	var cause error
	switch typed := request.(type) {
	case *protocol.CancelRequest:
		if typed.Arguments != nil && typed.Arguments.RequestId != 0 {
			if target := s.findLaunchLocked(typed.Arguments.RequestId); target != nil {
				envelope.targets = []*launchAttempt{target}
				cause = errLaunchCancelled
			}
		}
	case *protocol.TerminateRequest:
		if typed.Arguments == nil || !typed.Arguments.Restart {
			envelope.targets = s.controlLaunchesLocked()
			cause = errLaunchTerminated
		}
	case *protocol.DisconnectRequest:
		if typed.Arguments == nil || (!typed.Arguments.Restart && !typed.Arguments.SuspendDebuggee) {
			envelope.targets = s.controlLaunchesLocked()
			cause = errLaunchDisconnected
		}
	}

	var interrupt context.CancelCauseFunc

	if cause != nil {
		for _, target := range envelope.targets {
			if target.cause == nil {
				target.cause = cause
			}

			if s.activeLaunch == target {
				interrupt = s.activeCancel
			}
		}

		if _, disconnect := request.(*protocol.DisconnectRequest); disconnect {
			interrupt = s.activeCancel
		}
	}

	s.stateMu.Unlock()

	if interrupt != nil {
		interrupt(cause)
	}

	return envelope
}

func (s *Server) findLaunchLocked(sequence int) *launchAttempt {
	if s.pendingLaunch != nil && s.pendingLaunch.request.Seq == sequence {
		return s.pendingLaunch
	}

	for attempt := range s.queuedLaunches {
		if attempt.request.Seq == sequence {
			return attempt
		}
	}

	return nil
}

// Snapshot every launch preceding the control in the FIFO. An earlier attempt
// may roll back and admit the next one before dispatch reaches this control.
func (s *Server) controlLaunchesLocked() []*launchAttempt {
	targets := make([]*launchAttempt, 0, len(s.queuedLaunches)+1)
	if s.currentLaunch != nil {
		targets = append(targets, s.currentLaunch)
	}

	for attempt := range s.queuedLaunches {
		targets = append(targets, attempt)
	}

	return targets
}

func (s *Server) serveRequests(ctx context.Context, queue <-chan requestEnvelope) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.wake:
			if err := s.reconcileLaunch(); err != nil {
				return err
			}
		case envelope, ok := <-queue:
			if !ok || ctx.Err() != nil {
				return nil
			}

			if err := s.reconcileLaunch(); err != nil {
				return err
			}

			operationCtx, cancel := context.WithCancelCause(ctx)
			s.stateMu.Lock()
			target := s.currentLaunch

			if envelope.launch != nil {
				target = envelope.launch
			}

			s.activeLaunch = target
			s.activeCancel = cancel

			if target != nil && target.cause != nil {
				switch envelope.request.(type) {
				case *protocol.TerminateRequest, *protocol.DisconnectRequest, *protocol.CancelRequest:
				default:
					cancel(target.cause)
				}
			}

			s.stateMu.Unlock()
			err := s.dispatch(operationCtx, envelope.request, envelope.options, envelope.launch, envelope.targets)
			cancel(nil)
			s.stateMu.Lock()
			s.activeCancel = nil
			s.activeLaunch = nil
			disconnected := s.disconnected
			s.stateMu.Unlock()

			if err != nil {
				return err
			}

			if disconnected {
				return nil
			}

			if err := s.reconcileLaunch(); err != nil {
				return err
			}
		}
	}
}

// stop never waits for a handler, writer, watcher, or event lock. The dispatch
// owner rolls back before Run closes the service graph.
func (s *Server) stop(err error) {
	s.stateMu.Lock()
	if s.closing {
		s.stateMu.Unlock()

		return
	}

	s.closing = true
	s.failure = err
	cancel := s.runStop
	interrupt := s.activeCancel
	s.stateMu.Unlock()

	if interrupt != nil {
		interrupt(err)
	}

	if cancel != nil {
		cancel()
	}
}
