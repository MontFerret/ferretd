package client

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
)

type workspaceClientStub struct {
	openErr error
	getErr  error
}

func (s *workspaceClientStub) Open(
	context.Context,
	*workspacev1.OpenRequest,
	...grpc.CallOption,
) (*workspacev1.OpenResponse, error) {
	return nil, s.openErr
}

func (s *workspaceClientStub) Get(
	context.Context,
	*workspacev1.GetRequest,
	...grpc.CallOption,
) (*workspacev1.GetResponse, error) {
	return nil, s.getErr
}

func (*workspaceClientStub) List(
	context.Context,
	*workspacev1.ListRequest,
	...grpc.CallOption,
) (*workspacev1.ListResponse, error) {
	return &workspacev1.ListResponse{}, nil
}

func (*workspaceClientStub) Close(
	context.Context,
	*workspacev1.CloseRequest,
	...grpc.CallOption,
) (*workspacev1.CloseResponse, error) {
	return &workspacev1.CloseResponse{}, nil
}

func TestWorkspaceOpenMapsInvalidRoot(t *testing.T) {
	client := &WorkspaceClient{
		client: &workspaceClientStub{
			openErr: status.Error(codes.InvalidArgument, "invalid root"),
		},
	}

	_, err := client.Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrInvalidWorkspaceRoot) {
		t.Fatalf("Open error = %v, want ErrInvalidWorkspaceRoot", err)
	}
}

func TestWorkspaceGetMapsNotFound(t *testing.T) {
	client := &WorkspaceClient{
		client: &workspaceClientStub{
			getErr: status.Error(codes.NotFound, "workspace not found"),
		},
	}

	_, err := client.Get(context.Background(), "missing")
	if !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("Get error = %v, want ErrWorkspaceNotFound", err)
	}
}
