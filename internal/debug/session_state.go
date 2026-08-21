package debug

type (
	// State identifies a debug Session lifecycle state.
	State uint8

	// StopReason identifies why a debug Session is stopped.
	StopReason uint8
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

// Terminal reports whether state can no longer transition.
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateTerminated
}
