package client

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

type olderDaemonServer struct {
	daemonv1.UnimplementedDaemonServiceServer
	negotiations chan APIVersion
}

func (s *olderDaemonServer) GetInfo(_ context.Context, request *daemonv1.GetInfoRequest) (*daemonv1.GetInfoResponse, error) {
	if request.ClientApi == nil || request.ClientApi.Major != 1 {
		return nil, status.Error(codes.FailedPrecondition, "incompatible major version")
	}

	s.negotiations <- fromProtoAPIVersion(request.ClientApi)

	return &daemonv1.GetInfoResponse{ServerInfo: &daemonv1.ServerInfo{Version: "older", InstanceId: "older-instance", ApiVersion: &daemonv1.ApiVersion{Major: 1, Minor: 1}}}, nil
}

func (*olderDaemonServer) Shutdown(context.Context, *daemonv1.ShutdownRequest) (*daemonv1.ShutdownResponse, error) {
	return &daemonv1.ShutdownResponse{}, nil
}
