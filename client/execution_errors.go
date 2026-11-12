package client

import (
	"errors"
	"fmt"
)

var (
	errInvalidSessionRuntime      = errors.New("daemon returned an invalid session runtime acknowledgment")
	errIncompleteExecutionSession = errors.New("daemon returned an incomplete execution session")
	errIncompleteExecution        = errors.New("daemon returned an incomplete execution")
	errIncompleteExecutionEvent   = errors.New("daemon returned an incomplete execution event")

	// ErrExecutionSourceNotFound reports a missing retained source document.
	ErrExecutionSourceNotFound = errors.New("execution source not found")
	// ErrExecutionSourceClosed reports a workspace that can no longer compile sources.
	ErrExecutionSourceClosed = errors.New("execution source is closed")
	// ErrRuntimeNotFound reports an unknown selected execution runtime.
	ErrRuntimeNotFound = errors.New("runtime not found")
	// ErrRuntimeUnavailable reports failure to acquire the selected execution runtime.
	ErrRuntimeUnavailable = errors.New("runtime unavailable")
	// ErrSessionNotFound reports an unknown daemon Session.
	ErrSessionNotFound = errors.New("execution session not found")
	// ErrSessionClosed reports a Session that no longer accepts child Executions.
	ErrSessionClosed = errors.New("execution session is closed")
	// ErrExecutionNotFound reports an unknown daemon Execution.
	ErrExecutionNotFound = errors.New("execution not found")
	// ErrInvalidExecutionState reports an invalid lifecycle transition.
	ErrInvalidExecutionState = errors.New("invalid execution state")
	// ErrInvalidExecutionParameters reports parameter values rejected by the runtime boundary.
	ErrInvalidExecutionParameters = errors.New("invalid execution parameters")
	// ErrInvalidExecutionOptions reports invocation settings rejected by the runtime boundary.
	ErrInvalidExecutionOptions = errors.New("invalid execution options")
	// ErrExecutionWatcherLagged reports a watcher disconnected after its buffer overflowed.
	ErrExecutionWatcherLagged = errors.New("execution watcher lagged")
	// ErrExecutionServiceClosed reports a daemon execution manager that is shutting down.
	ErrExecutionServiceClosed = errors.New("execution service is closed")
	// ErrCompilationFailed reports runtime preparation failure without creating a Session.
	ErrCompilationFailed = errors.New("execution compilation failed")
)

// CompilationError retains diagnostics and the cause of failed Session preparation.
type CompilationError struct {
	Source      SourceSnapshot
	Diagnostics []Diagnostic
	cause       error
	message     string
}

// Error describes the source that failed compilation.
func (e *CompilationError) Error() string {
	if len(e.Diagnostics) == 0 && e.message != "" {
		return e.message
	}

	return fmt.Sprintf("%v: %s", ErrCompilationFailed, e.Source.RelativePath)
}

// Unwrap exposes both the stable classification and original RPC error.
func (e *CompilationError) Unwrap() []error {
	return []error{ErrCompilationFailed, e.cause}
}
