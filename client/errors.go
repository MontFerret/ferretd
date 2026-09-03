package client

import (
	"errors"
	"fmt"
)

var (
	errIncompleteServerInfo = errors.New("daemon returned incomplete server information")
	errIncompleteWorkspace  = errors.New("daemon returned an incomplete workspace")

	// ErrIncompatibleAPI reports a daemon with a different API major version.
	ErrIncompatibleAPI = errors.New("incompatible daemon API")
	// ErrDaemonUnavailable reports a daemon endpoint that cannot serve requests.
	ErrDaemonUnavailable = errors.New("daemon unavailable")
	// ErrInvalidEndpoint reports a malformed or unsupported endpoint.
	ErrInvalidEndpoint = errors.New("invalid daemon endpoint")
	// ErrInvalidBearerToken reports an empty bearer token option.
	ErrInvalidBearerToken = errors.New("invalid bearer token")
	// ErrInvalidWorkspaceRoot reports a root rejected by the daemon.
	ErrInvalidWorkspaceRoot = errors.New("invalid workspace root")
	// ErrWorkspaceNotFound reports an unknown workspace ID.
	ErrWorkspaceNotFound = errors.New("workspace not found")
)

// IncompatibleAPIError describes both sides of a failed API negotiation.
type IncompatibleAPIError struct {
	Client APIVersion
	Server APIVersion
}

// Error describes an API major-version mismatch.
func (e *IncompatibleAPIError) Error() string {
	return fmt.Sprintf(
		"%v: client %d.%d, server %d.%d",
		ErrIncompatibleAPI,
		e.Client.Major,
		e.Client.Minor,
		e.Server.Major,
		e.Server.Minor,
	)
}

// Unwrap exposes the stable incompatibility classification.
func (e *IncompatibleAPIError) Unwrap() error {
	return ErrIncompatibleAPI
}
