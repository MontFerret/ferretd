package exec

import (
	"errors"
	"fmt"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

var (
	errNilWorkspaceManager = errors.New("execution: nil workspace manager")
	errNilRuntimeOwner     = errors.New("execution: nil runtime owner")
	errExecutionCanceled   = errors.New("execution cancellation requested")

	// ErrClosed reports an execution service that is shutting down.
	ErrClosed = errors.New("execution service closed")
	// ErrDuplicateAcquisition reports reuse of a live acquisition key.
	ErrDuplicateAcquisition = errors.New("session acquisition ID already in use")
	// ErrSessionNotFound reports an unknown daemon Session ID.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionClosed reports a daemon Session that cannot create new Executions.
	ErrSessionClosed = errors.New("session closed")
	// ErrExecutionNotFound reports an unknown daemon Execution ID.
	ErrExecutionNotFound = errors.New("execution not found")
	// ErrExecutionRunning reports an Execution that has already started.
	ErrExecutionRunning = errors.New("execution already running")
	// ErrExecutionTerminal reports an Execution that has already terminated.
	ErrExecutionTerminal = errors.New("execution already terminal")
	// ErrInvalidExecutionOptions reports invocation settings rejected before execution creation.
	ErrInvalidExecutionOptions = errors.New("invalid execution options")
	// ErrWatcherLagged reports a watcher that exceeded its bounded event buffer.
	ErrWatcherLagged = errors.New("execution watcher lagged")
	// ErrCompilationFailed classifies runtime compilation and metadata failures.
	ErrCompilationFailed = errors.New("session compilation failed")
)

// CompilationError retains structured runtime compiler diagnostics.
type CompilationError struct {
	Source      workspace.SourceSnapshot
	Diagnostics []diagnostic.Diagnostic
	Cause       error
}

// Error describes a failed compilation while preserving its stable classification.
func (e *CompilationError) Error() string {
	if len(e.Diagnostics) == 0 && e.Cause != nil {
		return fmt.Sprintf("%v: %s: %v", ErrCompilationFailed, e.Source.RelativePath, e.Cause)
	}

	return fmt.Sprintf("%v: %s", ErrCompilationFailed, e.Source.RelativePath)
}

// Unwrap exposes the compiler cause and stable classification.
func (e *CompilationError) Unwrap() []error {
	return []error{ErrCompilationFailed, e.Cause}
}
