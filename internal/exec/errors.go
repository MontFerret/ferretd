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
	// ErrDebugSessionNotFound reports an unknown daemon DebugSession ID.
	ErrDebugSessionNotFound = errors.New("debug session not found")
	// ErrDebugSessionRunning reports a DebugSession with an active command.
	ErrDebugSessionRunning = errors.New("debug session running")
	// ErrDebugSessionNotRunning reports a DebugSession without an active command.
	ErrDebugSessionNotRunning = errors.New("debug session not running")
	// ErrDebugSessionNotStopped reports a DebugSession that cannot be inspected.
	ErrDebugSessionNotStopped = errors.New("debug session not stopped")
	// ErrDebugSessionTerminal reports a terminal DebugSession.
	ErrDebugSessionTerminal = errors.New("debug session terminal")
	// ErrDebugWatcherLagged reports a debug watcher that exceeded its bounded buffer.
	ErrDebugWatcherLagged = errors.New("debug session watcher lagged")
	// ErrDebugSourceChanged reports debug compilation from a different source snapshot.
	ErrDebugSourceChanged = errors.New("debug compilation source changed")
)
