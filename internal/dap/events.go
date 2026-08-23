package dap

import (
	"context"
	"fmt"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

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
	for event := range subscription.Events {
		s.eventMu.Lock()
		s.handleDebugEvent(event)
		s.eventMu.Unlock()
	}

	for err := range subscription.Errors {
		if err == nil {
			continue
		}

		s.logger.Error().Err(err).Msg("DAP debug session watch failed")

		s.eventMu.Lock()
		if sendErr := s.sendOutput(outputCategoryStderr, fmt.Sprintf("debug session watch failed: %v\n", err)); sendErr != nil {
			s.logger.Error().Err(sendErr).Msg("send DAP output event failed")
		}

		if sendErr := s.sendTerminated(); sendErr != nil {
			s.logger.Error().Err(sendErr).Msg("send DAP terminated event failed")
		}
		s.eventMu.Unlock()
	}
}

func (s *Server) handleDebugEvent(event debug.Event) {
	snapshot := event.Snapshot
	switch snapshot.State {
	case debug.StateRunning:
		s.handles.Reset()
		s.logger.Info().Msg("DAP execution running")
	case debug.StateStopped:
		s.stateMu.Lock()
		suppress := s.suppressEntry && snapshot.Reason == debug.StopEntry
		if suppress {
			s.suppressEntry = false
		}
		s.stateMu.Unlock()
		if suppress {
			s.logger.Info().
				Str("reason", stopReasonEntry).
				Int("thread_id", threadID).
				Bool("suppressed", true).
				Msg("DAP execution stopped")
			s.handles.Reset()

			if _, err := s.debugs.ContinueSession(context.Background(), snapshot.ID); err != nil {
				s.logger.Error().Err(err).Msg("DAP continue after entry failed")
				if sendErr := s.sendOutput(outputCategoryStderr, fmt.Sprintf("continue after entry failed: %v\n", err)); sendErr != nil {
					s.logger.Error().Err(sendErr).Msg("send DAP output event failed")
				}

				if sendErr := s.sendTerminated(); sendErr != nil {
					s.logger.Error().Err(sendErr).Msg("send DAP terminated event failed")
				}
			}

			return
		}

		if err := s.sendStopped(snapshot); err != nil {
			s.logger.Error().Err(err).Msg("send DAP stopped event failed")
		}
	case debug.StateCompleted:
		s.handles.Reset()
		s.logger.Info().Msg("DAP execution completed")

		if snapshot.Output != nil && len(snapshot.Output.Content) > 0 {
			if err := s.sendOutput(outputCategoryStdout, ensureTrailingNewline(string(snapshot.Output.Content))); err != nil {
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
		s.handles.Reset()
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
		s.handles.Reset()

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
	case debug.StopEntry:
		reason = stopReasonEntry
	case debug.StopBreakpoint:
		reason = stopReasonBreakpoint
	case debug.StopStep:
		reason = stopReasonStep
	case debug.StopPause:
		reason = stopReasonPause
	case debug.StopRuntimeError:
		reason = stopReasonException
		if snapshot.Failure != nil {
			description = snapshot.Failure.Message
		}
	}

	hitIDs := make([]int, len(snapshot.HitBreakpointIDs))
	for index, id := range snapshot.HitBreakpointIDs {
		hitIDs[index] = s.dapBreakpointID(id)
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
	return s.sendEvent(eventTerminated, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.TerminatedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventTerminated},
		}
	})
}
