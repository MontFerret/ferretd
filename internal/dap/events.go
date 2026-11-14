package dap

import (
	"context"
	"errors"
	"fmt"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/debug"
)

const (
	threadID   = 1
	threadName = "Ferret"

	eventInitialized = "initialized"
	eventStopped     = "stopped"
	eventOutput      = "output"
	eventExited      = "exited"
	eventTerminated  = "terminated"

	stopReasonEntry      = "entry"
	stopReasonBreakpoint = "breakpoint"
	stopReasonStep       = "step"
	stopReasonPause      = "pause"
	stopReasonException  = "exception"

	outputCategoryStdout = "stdout"
	outputCategoryStderr = "stderr"
)

func (s *Server) watchDebugSession(subscription debug.Subscription) {
	defer close(s.watchDone)
	defer func() {
		s.eventMu.Lock()
		s.initialStop = nil
		s.breakpoints.finish()
		s.eventMu.Unlock()
	}()

	for {
		select {
		case <-s.watchStop:
			return
		case event, ok := <-subscription.Events:
			if !ok {
				for err := range subscription.Errors {
					if err != nil {
						s.failWatch(fmt.Errorf("debug session watch failed: %w", err))
					}
				}

				return
			}

			s.eventMu.Lock()
			s.stateMu.Lock()
			closed := s.ownedClosed || s.closing || (s.pendingLaunch != nil && s.pendingLaunch.cause != nil)
			s.stateMu.Unlock()

			if !closed {
				s.handleDebugEvent(event)
			}

			s.eventMu.Unlock()
		}
	}
}

func (s *Server) handleDebugEvent(event debug.Event) {
	snapshot := event.Snapshot
	if snapshot.State.Terminal() {
		s.initialStop = nil
	}

	switch snapshot.State {
	case debug.StateRunning:
		s.invalidateHandles("running")
		s.logger.Info().Msg("DAP execution running")
	case debug.StateStopped:
		suppress := false

		if s.initialStop != nil {
			snapshot, suppress = s.initialStop.resolve(snapshot, s.owned.stopOnEntry, s.breakpoints)
			s.initialStop = nil
		}

		if suppress {
			s.breakpoints.commandStopped()
			s.logger.Info().
				Str("reason", stopReasonEntry).
				Int("thread_id", threadID).
				Bool("suppressed", true).
				Msg("DAP execution stopped")
			s.invalidateHandles("suppressed entry")

			if _, err := s.debugs.ContinueSession(context.Background(), snapshot.ID); err != nil {
				// Loss can commit after entry was queued. The terminal snapshot
				// owns its output, exit code and termination ordering.
				if errors.Is(err, debug.ErrSessionTerminal) {
					return
				}

				s.logger.Error().Err(err).Msg("DAP continue after entry failed")

				if sendErr := s.sendOutput(outputCategoryStderr, fmt.Sprintf("continue after entry failed: %v\n", err)); sendErr != nil {
					s.logger.Error().Err(sendErr).Msg("send DAP output event failed")
				}

				if sendErr := s.sendTerminated(); sendErr != nil {
					s.logger.Error().Err(sendErr).Msg("send DAP terminated event failed")
				}
			} else {
				s.breakpoints.commandStarted()
			}

			return
		}

		if err := s.sendStopped(snapshot); err != nil {
			s.logger.Error().Err(err).Msg("send DAP stopped event failed")
		}

		s.breakpoints.commandStopped()
	case debug.StateCompleted:
		defer s.breakpoints.finish()
		s.invalidateHandles("completed")
		s.logger.Info().Msg("DAP execution completed")

		if snapshot.Output != nil && len(snapshot.Output.Data) > 0 {
			if err := s.sendOutput(outputCategoryStdout, ensureTrailingNewline(string(snapshot.Output.Data))); err != nil {
				s.logger.Error().Err(err).Msg("send DAP output event failed")
			}
		}

		if err := s.sendExited(0); err != nil {
			s.logger.Error().Err(err).Msg("send DAP exited event failed")
		}

		if err := s.sendTerminated(); err != nil {
			s.logger.Error().Err(err).Msg("send DAP terminated event failed")
		}
	case debug.StateFailed:
		defer s.breakpoints.finish()
		s.invalidateHandles("failed")

		if snapshot.Output != nil && len(snapshot.Output.Data) > 0 {
			if err := s.sendOutput(outputCategoryStdout, ensureTrailingNewline(string(snapshot.Output.Data))); err != nil {
				s.logger.Error().Err(err).Msg("send DAP output event failed")
			}
		}

		message := "debug execution failed"

		if snapshot.Failure != nil && snapshot.Failure.Message != "" {
			message = snapshot.Failure.Message
		}

		s.logger.Error().Str("error", message).Msg("DAP execution failed")

		if err := s.sendOutput(outputCategoryStderr, ensureTrailingNewline(message)); err != nil {
			s.logger.Error().Err(err).Msg("send DAP output event failed")
		}

		if err := s.sendExited(1); err != nil {
			s.logger.Error().Err(err).Msg("send DAP exited event failed")
		}

		if err := s.sendTerminated(); err != nil {
			s.logger.Error().Err(err).Msg("send DAP terminated event failed")
		}
	case debug.StateTerminated:
		defer s.breakpoints.finish()
		s.invalidateHandles("terminated")

		s.logger.Info().Msg("DAP execution terminated")

		if err := s.sendTerminated(); err != nil {
			s.logger.Error().Err(err).Msg("send DAP terminated event failed")
		}
	}
}

func (s *Server) sendStopped(snapshot debug.SessionSnapshot) error {
	reason := stopReasonPause
	description := ""
	switch snapshot.Reason {
	case apidebugger.ReasonEntry:
		reason = stopReasonEntry
	case apidebugger.ReasonBreakpoint:
		reason = stopReasonBreakpoint
	case apidebugger.ReasonStep:
		reason = stopReasonStep
	case apidebugger.ReasonPause:
		reason = stopReasonPause
	case apidebugger.ReasonRuntimeError:
		reason = stopReasonException

		if snapshot.Failure != nil {
			description = snapshot.Failure.Message
		}
	}

	hitIDs := make([]int, len(snapshot.HitBreakpointIDs))
	for index, id := range snapshot.HitBreakpointIDs {
		hitIDs[index] = s.breakpoints.id(id)
	}

	s.logger.Info().
		Str("reason", reason).
		Int("thread_id", threadID).
		Int("hit_breakpoints", len(hitIDs)).
		Msg("DAP execution stopped")

	return s.sendEvent(eventStopped, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.StoppedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventStopped},
			Body: protocol.StoppedEventBody{
				Reason:            reason,
				Description:       description,
				ThreadId:          threadID,
				AllThreadsStopped: true,
				HitBreakpointIds:  hitIDs,
			},
		}
	}, func(event *zerolog.Event) {
		event.
			Str("reason", reason).
			Int("thread_id", threadID).
			Int("hit_breakpoints", len(hitIDs))
	})
}

func (s *Server) sendOutput(category, output string) error {
	return s.sendEvent(eventOutput, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.OutputEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventOutput},
			Body:  protocol.OutputEventBody{Category: category, Output: output},
		}
	}, func(event *zerolog.Event) {
		event.Str("category", category).Int("bytes", len(output))
	})
}

func (s *Server) sendExited(code int) error {
	return s.sendEvent(eventExited, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.ExitedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventExited},
			Body:  protocol.ExitedEventBody{ExitCode: code},
		}
	}, func(event *zerolog.Event) {
		event.Int("exit_code", code)
	})
}

func (s *Server) sendTerminated() error {
	if s.terminalSent {
		return nil
	}

	s.terminalSent = true

	return s.sendEvent(eventTerminated, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.TerminatedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventTerminated},
		}
	})
}

// failWatch signals dispatch before any event-lock acquisition. A blocked
// request must release eventMu before dispatch can settle the failed watch.
func (s *Server) failWatch(err error) {
	s.stateMu.Lock()

	s.watchFailure = errors.Join(s.watchFailure, err)
	if s.pendingLaunch != nil && s.pendingLaunch.cause == nil {
		s.pendingLaunch.cause = err
	}

	interrupt := s.activeCancel
	s.stateMu.Unlock()

	if interrupt != nil {
		interrupt(err)
	}

	select {
	case s.wake <- struct{}{}:
	default:
	}
}
