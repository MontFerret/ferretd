package client

import (
	"context"

	"google.golang.org/grpc"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

type runtimeClientStub struct {
	daemonv1.DaemonServiceClient
	response *daemonv1.ListRuntimesResponse
	err      error
	calls    int
}

func (s *runtimeClientStub) ListRuntimes(context.Context, *daemonv1.ListRuntimesRequest, ...grpc.CallOption) (*daemonv1.ListRuntimesResponse, error) {
	s.calls++

	return s.response, s.err
}
