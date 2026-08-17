package exec

import "github.com/MontFerret/ferretd/internal/diagnostic"

type (
	// DebugSessionID is an opaque daemon DebugSession identifier.
	DebugSessionID string

	// DebugState identifies a DebugSession lifecycle state.
	DebugState uint8

	// DebugStopReason identifies why a DebugSession is stopped.
	DebugStopReason uint8

	// DebugEventKind identifies an ordered DebugSession lifecycle event.
	DebugEventKind uint8

	// DebugScopeKind identifies a transport-neutral debugger scope.
	DebugScopeKind uint8

	// DebugValueReference identifies an expandable value in one paused state.
	DebugValueReference uint64

	// DebugSessionOptions contains invocation-specific debugger settings.
	DebugSessionOptions struct {
		OutputContentType string
	}

	// DebugSessionSnapshot is an immutable view of one daemon DebugSession.
	DebugSessionSnapshot struct {
		ID               DebugSessionID
		Session          SessionID
		State            DebugState
		Reason           DebugStopReason
		Location         DebugLocation
		HitBreakpointIDs []uint64
		Parameters       map[string]any
		Options          DebugSessionOptions
		Output           *Output
		Failure          *DebugFailure
	}

	// DebugLocation identifies a 1-based source position.
	DebugLocation struct {
		File   string
		Line   int
		Column int
	}

	// DebugBreakpointLocation identifies a requested 1-based source position.
	DebugBreakpointLocation struct {
		Line   int
		Column int
	}

	// DebugBreakpoint describes a requested and resolved source breakpoint.
	DebugBreakpoint struct {
		ID              uint64
		File            string
		RequestedLine   int
		RequestedColumn int
		Line            int
		Column          int
		Verified        bool
	}

	// DebugFrame describes one paused frame. Index zero is the current frame.
	DebugFrame struct {
		Index    int
		Name     string
		Location DebugLocation
	}

	// DebugScope groups variables visible in one frame.
	DebugScope struct {
		Kind      DebugScopeKind
		Name      string
		Variables []DebugVariable
	}

	// DebugValue is a bounded, debugger-safe value representation.
	DebugValue struct {
		Type      string
		Display   string
		Reference DebugValueReference
	}

	// DebugVariable is one visible local or parameter.
	DebugVariable struct {
		Name    string
		Value   DebugValue
		Mutable bool
	}

	// DebugFailure retains durable debugger failure information.
	DebugFailure struct {
		Message     string
		Diagnostics []diagnostic.Diagnostic
	}

	// DebugEvent is one ordered lifecycle observation for a DebugSession.
	DebugEvent struct {
		DebugSession DebugSessionID
		Sequence     uint64
		Kind         DebugEventKind
		Snapshot     DebugSessionSnapshot
	}

	// DebugSubscription provides the latest event and bounded future observations.
	DebugSubscription struct {
		Current DebugEvent
		Events  <-chan DebugEvent
		Errors  <-chan error
		Cancel  func()
	}
)

const (
	DebugStateCreated DebugState = iota + 1
	DebugStateRunning
	DebugStateStopped
	DebugStateCompleted
	DebugStateFailed
	DebugStateTerminated
)

const (
	DebugStopNone DebugStopReason = iota
	DebugStopEntry
	DebugStopBreakpoint
	DebugStopStep
	DebugStopPause
	DebugStopRuntimeError
)

const (
	DebugEventCreated DebugEventKind = iota + 1
	DebugEventRunning
	DebugEventStopped
	DebugEventCompleted
	DebugEventFailed
	DebugEventTerminated
)

const (
	DebugScopeLocals DebugScopeKind = iota + 1
	DebugScopeParameters
)

// Terminal reports whether state can no longer transition.
func (s DebugState) Terminal() bool {
	return s == DebugStateCompleted || s == DebugStateFailed || s == DebugStateTerminated
}
