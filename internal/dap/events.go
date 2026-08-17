package dap

import (
	"context"
	"fmt"
	"strings"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/exec"
)

func (s *Server) watchDebugSession(subscription exec.DebugSubscription) {
	for event := range subscription.Events {
		s.eventMu.Lock()
		s.handleDebugEvent(event)
		s.eventMu.Unlock()
	}

	for err := range subscription.Errors {
		if err == nil {
			continue
		}

		s.eventMu.Lock()
		_ = s.sendOutput("stderr", fmt.Sprintf("debug session watch failed: %v\n", err))
		_ = s.sendTerminated()
		s.eventMu.Unlock()
	}
}

func (s *Server) handleDebugEvent(event exec.DebugEvent) {
	snapshot := event.Snapshot
	switch snapshot.State {
	case exec.DebugStateRunning:
		s.handles.Reset()
	case exec.DebugStateStopped:
		s.stateMu.Lock()
		suppress := s.suppressEntry && snapshot.Reason == exec.DebugStopEntry
		if suppress {
			s.suppressEntry = false
		}
		s.stateMu.Unlock()
		if suppress {
			s.handles.Reset()
			if _, err := s.executions.ContinueDebugSession(context.Background(), snapshot.ID); err != nil {
				_ = s.sendOutput("stderr", fmt.Sprintf("continue after entry failed: %v\n", err))
				_ = s.sendTerminated()
			}

			return
		}

		_ = s.sendStopped(snapshot)
	case exec.DebugStateCompleted:
		s.handles.Reset()
		if snapshot.Output != nil && len(snapshot.Output.Content) > 0 {
			_ = s.sendOutput("stdout", ensureTrailingNewline(string(snapshot.Output.Content)))
		}
		_ = s.sendExited(0)
		_ = s.sendTerminated()
	case exec.DebugStateFailed:
		s.handles.Reset()
		message := "debug execution failed"
		if snapshot.Failure != nil && snapshot.Failure.Message != "" {
			message = snapshot.Failure.Message
		}
		_ = s.sendOutput("stderr", ensureTrailingNewline(message))
		_ = s.sendExited(1)
		_ = s.sendTerminated()
	case exec.DebugStateTerminated:
		s.handles.Reset()
		_ = s.sendTerminated()
	}
}

func (s *Server) sendStopped(snapshot exec.DebugSessionSnapshot) error {
	reason := "pause"
	description := ""
	switch snapshot.Reason {
	case exec.DebugStopEntry:
		reason = "entry"
	case exec.DebugStopBreakpoint:
		reason = "breakpoint"
	case exec.DebugStopStep:
		reason = "step"
	case exec.DebugStopPause:
		reason = "pause"
	case exec.DebugStopRuntimeError:
		reason = "exception"
		if snapshot.Failure != nil {
			description = snapshot.Failure.Message
		}
	}

	hitIDs := make([]int, len(snapshot.HitBreakpointIDs))
	for index, id := range snapshot.HitBreakpointIDs {
		hitIDs[index] = s.dapBreakpointID(id)
	}

	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.StoppedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: "stopped"},
			Body: protocol.StoppedEventBody{
				Reason:            reason,
				Description:       description,
				ThreadId:          threadID,
				AllThreadsStopped: true,
				HitBreakpointIds:  hitIDs,
			},
		}
	})
}

func (s *Server) sendOutput(category, output string) error {
	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.OutputEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: "output"},
			Body:  protocol.OutputEventBody{Category: category, Output: output},
		}
	})
}

func (s *Server) sendExited(code int) error {
	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.ExitedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: "exited"},
			Body:  protocol.ExitedEventBody{ExitCode: code},
		}
	})
}

func (s *Server) sendTerminated() error {
	return s.send(func(base protocol.ProtocolMessage) protocol.Message {
		return &protocol.TerminatedEvent{
			Event: protocol.Event{ProtocolMessage: base, Event: "terminated"},
		}
	})
}

func ensureTrailingNewline(value string) string {
	if strings.HasSuffix(value, "\n") {
		return value
	}

	return value + "\n"
}
