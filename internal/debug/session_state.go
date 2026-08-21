package debug

type (
	// State identifies a debug Session lifecycle state.
	State uint8

	// StopReason identifies why a debug Session is stopped.
	StopReason uint8
)

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

const (
	// StopNone is the natural zero value when no stop reason applies.
	StopNone StopReason = iota
	// StopEntry reports the debugger's initial entry suspension.
	StopEntry
	// StopBreakpoint reports a source breakpoint hit.
	StopBreakpoint
	// StopStep reports completion of a debugger step command.
	StopStep
	// StopPause reports an explicit pause request.
	StopPause
	// StopRuntimeError reports suspension at a runtime failure.
	StopRuntimeError
)

// Terminal reports whether state can no longer transition.
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateTerminated
}
