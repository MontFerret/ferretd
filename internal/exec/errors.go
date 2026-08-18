package exec

import "errors"

var (
	errExecutionCanceled = errors.New("execution cancellation requested")

	// ErrManagerClosed reports an execution manager that is shutting down.
	ErrManagerClosed = errors.New("execution manager closed")
	// ErrWorkspaceClosed reports a workspace whose execution children are closing.
	ErrWorkspaceClosed = errors.New("workspace closed")
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
	// ErrWatcherLagged reports a watcher that exceeded its bounded event buffer.
	ErrWatcherLagged = errors.New("execution watcher lagged")
	// ErrCompilationFailed classifies Ferret compiler diagnostics.
	ErrCompilationFailed = errors.New("session compilation failed")
	// ErrDebugSourceChanged reports debug compilation from a different source snapshot.
	ErrDebugSourceChanged = errors.New("debug compilation source changed")
)
