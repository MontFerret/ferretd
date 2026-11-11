package runtimes

import "errors"

var (
	errNilContext = errors.New("runtime owner: nil context")

	// ErrClosed classifies acquisitions rejected by an owner that is shutting down.
	ErrClosed = errors.New("runtime owner closed")
	// ErrInvalidConfig classifies malformed or invalid runtime configuration.
	ErrInvalidConfig = errors.New("invalid runtime configuration")
	// ErrRuntimeNotFound classifies unresolved explicit or default target names.
	ErrRuntimeNotFound = errors.New("runtime not found")
	// ErrRuntimeUnavailable classifies runtime acquisition failures, preserving their cause.
	ErrRuntimeUnavailable = errors.New("runtime unavailable")
)
