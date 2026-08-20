// Package exec coordinates daemon-owned Ferret Plans and one-shot Executions.
package exec

import (
	"fmt"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	// State identifies an Execution lifecycle state.
	State uint8

	// FailureCategory classifies a failed execution phase.
	FailureCategory uint8

	// EventKind identifies a strongly typed execution lifecycle event.
	EventKind uint8

	// SessionSnapshot is an immutable view of a daemon Session.
	SessionSnapshot struct {
		ID         SessionID
		Source     workspace.SourceSnapshot
		Parameters []string
	}

	// Output is a defensively copied encoded Ferret result.
	Output struct {
		ContentType string
		Content     []byte
	}

	// Failure retains useful execution failure information.
	Failure struct {
		Category    FailureCategory
		Message     string
		Diagnostics []diagnostic.Diagnostic
	}

	// CompilationError retains structured Ferret compiler diagnostics.
	CompilationError struct {
		Source      workspace.SourceSnapshot
		Diagnostics []diagnostic.Diagnostic
		Cause       error
	}
)

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

const (
	// FailureSessionCreation identifies Plan.NewSession failure.
	FailureSessionCreation FailureCategory = iota + 1
	// FailureRuntime identifies Ferret Session.Run failure.
	FailureRuntime
	// FailureCleanup identifies Ferret Session cleanup failure.
	FailureCleanup
)

const (
	// EventCreated reports creation of an Execution resource.
	EventCreated EventKind = iota + 1
	// EventStarted reports the start of the one-shot invocation.
	EventStarted
	// EventCompleted reports successful termination.
	EventCompleted
	// EventFailed reports failed termination.
	EventFailed
	// EventCancelled reports cancellation termination.
	EventCancelled
)

// Error describes a failed compilation while preserving its stable classification.
func (e *CompilationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrCompilationFailed, e.Source.RelativePath)
}

// Unwrap exposes the compiler cause and stable classification.
func (e *CompilationError) Unwrap() []error {
	return []error{ErrCompilationFailed, e.Cause}
}

// Terminal reports whether state can no longer transition.
func (s State) Terminal() bool {
	return s == StateCompleted || s == StateFailed || s == StateCancelled
}

func cloneParameters(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = cloneParameterValue(value)
	}

	return result
}

func cloneParameterValue(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = cloneParameterValue(typed[i])
		}

		return result
	case map[string]any:
		return cloneParameters(typed)
	default:
		return typed
	}
}
