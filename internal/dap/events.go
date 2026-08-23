package dap

import (
	"context"
	"fmt"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/logging"
)

const (
	threadID   = 1
	threadName = "Ferret"
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

		s.logger.Error().Err(err).Msg(logMessageDebugSessionWatchFailed)

		s.eventMu.Lock()
		if sendErr := s.sendOutput(outputCategoryStderr, fmt.Sprintf("debug session watch failed: %v\n", err)); sendErr != nil {
			s.logger.Error().Err(sendErr).Msg(logMessageSendOutputEventFailed)
		}

		if sendErr := s.sendTerminated(); sendErr != nil {
			s.logger.Error().Err(sendErr).Msg(logMessageSendTerminatedEventFailed)
		}
		s.eventMu.Unlock()
	}
}

func (s *Server) handleDebugEvent(event debug.Event) {
	snapshot := event.Snapshot
	switch snapshot.State {
	case debug.StateRunning:
		s.handles.Reset()
		s.logger.Info().Msg(logMessageExecutionRunning)
	case debug.StateStopped:
		s.stateMu.Lock()
		suppress := s.suppressEntry && snapshot.Reason == debug.StopEntry
		if suppress {
			s.suppressEntry = false
		}
		s.stateMu.Unlock()
		if suppress {
			s.logger.Info().
				Enum(logFieldReason, stopReasonEntry).
				Int(logFieldThreadID, threadID).
				Bool(logFieldSuppressed, true).
				Msg(logMessageExecutionStopped)
			s.handles.Reset()

			if _, err := s.debugs.ContinueSession(context.Background(), snapshot.ID); err != nil {
				s.logger.Error().Err(err).Msg(logMessageContinueAfterEntryFailed)
				if sendErr := s.sendOutput(outputCategoryStderr, fmt.Sprintf("continue after entry failed: %v\n", err)); sendErr != nil {
					s.logger.Error().Err(sendErr).Msg(logMessageSendOutputEventFailed)
				}

				if sendErr := s.sendTerminated(); sendErr != nil {
					s.logger.Error().Err(sendErr).Msg(logMessageSendTerminatedEventFailed)
				}
			}

			return
		}

		if err := s.sendStopped(snapshot); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendStoppedEventFailed)
		}
	case debug.StateCompleted:
		s.handles.Reset()
		s.logger.Info().Msg(logMessageExecutionCompleted)

		if snapshot.Output != nil && len(snapshot.Output.Content) > 0 {
			if err := s.sendOutput(outputCategoryStdout, ensureTrailingNewline(string(snapshot.Output.Content))); err != nil {
				s.logger.Error().Err(err).Msg(logMessageSendOutputEventFailed)
			}
		}

		if err := s.sendExited(0); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendExitedEventFailed)
		}

		if err := s.sendTerminated(); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendTerminatedEventFailed)
		}
	case debug.StateFailed:
		s.handles.Reset()
		message := "debug execution failed"

		if snapshot.Failure != nil && snapshot.Failure.Message != "" {
			message = snapshot.Failure.Message
		}

		s.logger.Error().
			String(logFieldError, message).
			Msg(logMessageExecutionFailed)

		if err := s.sendOutput(outputCategoryStderr, ensureTrailingNewline(message)); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendOutputEventFailed)
		}

		if err := s.sendExited(1); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendExitedEventFailed)
		}

		if err := s.sendTerminated(); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendTerminatedEventFailed)
		}
	case debug.StateTerminated:
		s.handles.Reset()

		s.logger.Info().Msg(logMessageExecutionTerminated)

		if err := s.sendTerminated(); err != nil {
			s.logger.Error().Err(err).Msg(logMessageSendTerminatedEventFailed)
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
		Enum(logFieldReason, reason).
		Int(logFieldThreadID, threadID).
		Int(logFieldHitBreakpoints, len(hitIDs)).
		Msg(logMessageExecutionStopped)

	return s.sendEvent(eventStopped, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.StoppedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventStopped},
			Body: protocol.StoppedEventBody{
				Reason:            reason.String(),
				Description:       description,
				ThreadId:          threadID,
				AllThreadsStopped: true,
				HitBreakpointIds:  hitIDs,
			},
		}
	}, func(record logging.Record) {
		record.
			Enum(logFieldReason, reason).
			Int(logFieldThreadID, threadID).
			Int(logFieldHitBreakpoints, len(hitIDs))
	})
}

func (s *Server) sendOutput(category outputCategory, output string) error {
	return s.sendEvent(eventOutput, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.OutputEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventOutput},
			Body:  protocol.OutputEventBody{Category: category.String(), Output: output},
		}
	}, func(record logging.Record) {
		record.Enum(logFieldCategory, category).Int(logFieldBytes, len(output))
	})
}

func (s *Server) sendExited(code int) error {
	return s.sendEvent(eventExited, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.ExitedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventExited},
			Body:  protocol.ExitedEventBody{ExitCode: code},
		}
	}, func(record logging.Record) {
		record.Int(logFieldExitCode, code)
	})
}

func (s *Server) sendTerminated() error {
	return s.sendEvent(eventTerminated, func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.TerminatedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: eventTerminated},
		}
	})
}
