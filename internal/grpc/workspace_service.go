package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type workspaceService struct {
	workspacev1.UnimplementedWorkspaceServiceServer
	workspaces *workspace.Manager
}

func newWorkspaceService(workspaces *workspace.Manager) (*workspaceService, error) {
	if workspaces == nil {
		return nil, errNilWorkspaceManager
	}

	return &workspaceService{workspaces: workspaces}, nil
}

func (s *workspaceService) Open(
	ctx context.Context,
	request *workspacev1.OpenRequest,
) (*workspacev1.OpenResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "workspace root is required")
	}

	result, err := s.workspaces.Open(ctx, request.Root)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &workspacev1.OpenResponse{Workspace: toProtoWorkspace(result)}, nil
}

func (s *workspaceService) Get(
	ctx context.Context,
	request *workspacev1.GetRequest,
) (*workspacev1.GetResponse, error) {
	if request == nil || request.Id == nil || request.Id.Value == "" {
		return nil, status.Error(codes.NotFound, "workspace not found")
	}

	result, err := s.workspaces.Get(ctx, workspace.ID(request.Id.Value))
	if err != nil {
		return nil, toStatusError(err)
	}

	return &workspacev1.GetResponse{Workspace: toProtoWorkspace(result)}, nil
}

func (s *workspaceService) List(
	ctx context.Context,
	_ *workspacev1.ListRequest,
) (*workspacev1.ListResponse, error) {
	items, err := s.workspaces.List(ctx)
	if err != nil {
		return nil, toStatusError(err)
	}

	result := &workspacev1.ListResponse{
		Workspaces: make([]*workspacev1.Workspace, 0, len(items)),
	}
	for _, item := range items {
		result.Workspaces = append(result.Workspaces, toProtoWorkspace(item))
	}

	return result, nil
}

func (s *workspaceService) Close(
	ctx context.Context,
	request *workspacev1.CloseRequest,
) (*workspacev1.CloseResponse, error) {
	if request == nil || request.Id == nil {
		return &workspacev1.CloseResponse{}, nil
	}

	if err := s.workspaces.Close(ctx, workspace.ID(request.Id.Value)); err != nil {
		return nil, toStatusError(err)
	}

	return &workspacev1.CloseResponse{}, nil
}
