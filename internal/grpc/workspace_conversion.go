package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func toWorkspaceStatusError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, workspace.ErrInvalidRoot):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, workspace.ErrLoad):
		return status.Error(codes.Internal, "workspace load failed")
	case errors.Is(err, workspace.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, "workspace operation failed")
	}
}

func toProtoWorkspace(value *workspace.Workspace) *workspacev1.Workspace {
	return &workspacev1.Workspace{
		Id:   &workspacev1.WorkspaceId{Value: value.ID().String()},
		Root: value.Root(),
	}
}
