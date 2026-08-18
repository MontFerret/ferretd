package debug

import "errors"

var (
	// ErrManagerClosed reports a debug manager that is shutting down.
	ErrManagerClosed = errors.New("debug manager closed")
	// ErrSessionNotFound reports an unknown daemon debug Session ID.
	ErrSessionNotFound = errors.New("debug session not found")
	// ErrSessionRunning reports a debug Session with an active command.
	ErrSessionRunning = errors.New("debug session running")
	// ErrSessionNotRunning reports a debug Session without an active command.
	ErrSessionNotRunning = errors.New("debug session not running")
	// ErrSessionNotStopped reports a debug Session that cannot be inspected.
	ErrSessionNotStopped = errors.New("debug session not stopped")
	// ErrSessionTerminal reports a terminal debug Session.
	ErrSessionTerminal = errors.New("debug session terminal")
	// ErrWatcherLagged reports a watcher that exceeded its bounded buffer.
	ErrWatcherLagged = errors.New("debug session watcher lagged")
)
