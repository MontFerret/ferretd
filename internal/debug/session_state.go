package debug

// State identifies a debug Session lifecycle state.
type State uint8

const (
	// StateCreated accepts configuration before execution starts.
	StateCreated State = iota + 1
	// StateRunning is actively executing debugger commands.
	StateRunning
	// StateStopped is suspended and available for inspection.
	StateStopped
	// StateCompleted is terminal successful execution.
	StateCompleted
	// StateFailed is terminal execution with a retained failure.
	StateFailed
	// StateTerminated is terminal cancellation requested by the debugger owner.
	StateTerminated
)

// Terminal reports whether state can no longer transition.
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateTerminated
}
