package exec

import "errors"

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
	// ErrWatcherLagged reports a watcher that exceeded its bounded event buffer.
	ErrWatcherLagged = errors.New("execution watcher lagged")
	// ErrCompilationFailed classifies Ferret compiler diagnostics.
	ErrCompilationFailed = errors.New("session compilation failed")
	// ErrDebugSourceChanged reports debug compilation from a different source snapshot.
	ErrDebugSourceChanged = errors.New("debug compilation source changed")
)
