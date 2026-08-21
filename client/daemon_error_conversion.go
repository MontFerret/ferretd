package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

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
