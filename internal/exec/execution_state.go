package exec

// State identifies an Execution lifecycle state.
type State uint8

const (
	// StateCreated identifies an Execution that has not started.
	StateCreated State = iota + 1
	// StateRunning identifies an active Execution.
	StateRunning
	// StateCompleted identifies a successful Execution.
	StateCompleted
	// StateFailed identifies a failed Execution.
	StateFailed
	// StateCancelled identifies an Execution stopped through cancellation.
	StateCancelled
)

// Terminal reports whether state can no longer transition.
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}
