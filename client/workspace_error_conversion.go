package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mapWorkspaceOpenError(ctx context.Context, err error) error {
	mapped := mapError(ctx, err)

	grpcStatus, ok := status.FromError(mapped)
	if !ok || grpcStatus.Code() != codes.InvalidArgument {
		return mapped
	}

	return fmt.Errorf("%w: %s", ErrInvalidWorkspaceRoot, grpcStatus.Message())
}

func mapWorkspaceGetError(ctx context.Context, err error) error {
	mapped := mapError(ctx, err)

	grpcStatus, ok := status.FromError(mapped)
	if !ok || grpcStatus.Code() != codes.NotFound {
		return mapped
	}

	return fmt.Errorf("%w: %s", ErrWorkspaceNotFound, grpcStatus.Message())
}
