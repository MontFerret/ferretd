// Package debug coordinates retained Ferret debugger sessions.
package debug

import (
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/exec"
)

type (
	// State identifies a debug Session lifecycle state.
	State uint8

	// StopReason identifies why a debug Session is stopped.
	StopReason uint8

	// EventKind identifies an ordered debug Session lifecycle event.
	EventKind uint8

	// ScopeKind identifies a transport-neutral debugger scope.
	ScopeKind uint8

	// ValueReference identifies an expandable value in one paused state.
	ValueReference uint64

	// SessionSnapshot is an immutable view of one daemon debug Session.
	SessionSnapshot struct {
		ID               SessionID
		Session          exec.SessionID
		State            State
		Reason           StopReason
		Location         Location
		HitBreakpointIDs []uint64
		Parameters       map[string]any
		Options          SessionOptions
		Output           *Output
		Failure          *Failure
	}

	// Output is the encoded terminal result of a completed debug Session.
	Output struct {
		ContentType string
		Content     []byte
	}

	// Location identifies a 1-based source position.
	Location struct {
		File   string
		Line   int
		Column int
	}

	// BreakpointLocation identifies a requested 1-based source position.
	BreakpointLocation struct {
		Line   int
		Column int
	}

	// Breakpoint describes a requested and resolved source breakpoint.
	Breakpoint struct {
		ID              uint64
		File            string
		RequestedLine   int
		RequestedColumn int
		Line            int
		Column          int
		Verified        bool
	}

	// Frame describes one paused frame. Index zero is the current frame.
	Frame struct {
		Index    int
		Name     string
		Location Location
	}

	// Scope groups variables visible in one frame.
	Scope struct {
		Kind      ScopeKind
		Name      string
		Variables []Variable
	}

	// Value is a bounded, debugger-safe value representation.
	Value struct {
		Type      string
		Display   string
		Reference ValueReference
	}

	// Variable is one visible local or parameter.
	Variable struct {
		Name    string
		Value   Value
		Mutable bool
	}

	// Failure retains durable debugger failure information.
	Failure struct {
		Message     string
		Diagnostics []diagnostic.Diagnostic
	}

	// Event is one ordered lifecycle observation for a debug Session.
	Event struct {
		Session  SessionID
		Sequence uint64
		Kind     EventKind
		Snapshot SessionSnapshot
	}

	// Subscription provides the latest event and bounded future observations.
	Subscription struct {
		Current Event
		Events  <-chan Event
		Errors  <-chan error
		Cancel  func()
	}
)

const (
	StateCreated State = iota + 1
	StateRunning
	StateStopped
	StateCompleted
	StateFailed
	StateTerminated
)

const (
	// StopNone is the natural zero value when no stop reason applies.
	StopNone StopReason = iota
	StopEntry
	StopBreakpoint
	StopStep
	StopPause
	StopRuntimeError
)

const (
	EventCreated EventKind = iota + 1
	EventRunning
	EventStopped
	EventCompleted
	EventFailed
	EventTerminated
)

const (
	ScopeLocals ScopeKind = iota + 1
	ScopeParameters
)

// Terminal reports whether state can no longer transition.
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateTerminated
}
