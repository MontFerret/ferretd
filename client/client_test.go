package client

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

type daemonClientStub struct {
	getInfoErr error
}

func (s *daemonClientStub) GetInfo(
	context.Context,
	*daemonv1.GetInfoRequest,
	...grpc.CallOption,
) (*daemonv1.GetInfoResponse, error) {
	return nil, s.getInfoErr
}

func (*daemonClientStub) Shutdown(
	context.Context,
	*daemonv1.ShutdownRequest,
	...grpc.CallOption,
) (*daemonv1.ShutdownResponse, error) {
	return &daemonv1.ShutdownResponse{}, nil
}

func TestInfoDoesNotMapInvalidArgumentToWorkspaceError(t *testing.T) {
	client := &Client{
		daemon: &daemonClientStub{
			getInfoErr: status.Error(codes.InvalidArgument, "client API version is required"),
		},
	}

	_, err := client.Info(context.Background())
	if errors.Is(err, ErrInvalidWorkspaceRoot) {
		t.Fatalf("Info error = %v, did not want ErrInvalidWorkspaceRoot", err)
	}

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Info code = %v, want InvalidArgument", status.Code(err))
	}
}
