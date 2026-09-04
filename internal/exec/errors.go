package exec

import (
	"errors"
	"fmt"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

var (
	errNilWorkspaceManager = errors.New("execution: nil workspace manager")
	errExecutionCanceled   = errors.New("execution cancellation requested")

	// ErrClosed reports an execution service that is shutting down.
	ErrClosed = errors.New("execution service closed")
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
	// ErrInvalidParameters reports parameter values Ferret cannot bind.
	ErrInvalidParameters = errors.New("invalid execution parameters")
	// ErrInvalidExecutionOptions reports invocation settings rejected before execution creation.
	ErrInvalidExecutionOptions = errors.New("invalid execution options")
	// ErrWatcherLagged reports a watcher that exceeded its bounded event buffer.
	ErrWatcherLagged = errors.New("execution watcher lagged")
	// ErrCompilationFailed classifies Ferret compiler diagnostics.
	ErrCompilationFailed = errors.New("session compilation failed")
	// ErrDebugSourceChanged reports debug compilation from a different source snapshot.
	ErrDebugSourceChanged = errors.New("debug compilation source changed")
)

// CompilationError retains structured Ferret compiler diagnostics.
type CompilationError struct {
	Source      workspace.SourceSnapshot
	Diagnostics []diagnostic.Diagnostic
	Cause       error
}

// Error describes a failed compilation while preserving its stable classification.
func (e *CompilationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrCompilationFailed, e.Source.RelativePath)
}

// Unwrap exposes the compiler cause and stable classification.
func (e *CompilationError) Unwrap() []error {
	return []error{ErrCompilationFailed, e.Cause}
}
