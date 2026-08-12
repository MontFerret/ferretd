package client

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
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

func mapError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}

	grpcStatus, ok := status.FromError(err)
	if !ok {
		return err
	}

	switch grpcStatus.Code() {
	case codes.Canceled:
		return context.Canceled
	case codes.DeadlineExceeded:
		return context.DeadlineExceeded
	case codes.FailedPrecondition:
		for _, detail := range grpcStatus.Details() {
			compatibility, ok := detail.(*daemonv1.ApiCompatibilityError)
			if !ok {
				continue
			}

			return &IncompatibleAPIError{
				Client: fromProtoAPIVersion(compatibility.ClientApi),
				Server: fromProtoAPIVersion(compatibility.ServerApi),
			}
		}

		return err
	case codes.Unavailable:
		return fmt.Errorf("%w: %s", ErrDaemonUnavailable, grpcStatus.Message())
	default:
		return err
	}
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
