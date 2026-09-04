package client

import (
	"context"

	"google.golang.org/grpc"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

type executionClientStub struct {
	executionv1.ExecutionServiceClient
	create func(context.Context, *executionv1.CreateExecutionRequest) (*executionv1.CreateExecutionResponse, error)
}

func (s *executionClientStub) CreateExecution(
	ctx context.Context,
	request *executionv1.CreateExecutionRequest,
	_ ...grpc.CallOption,
) (*executionv1.CreateExecutionResponse, error) {
	return s.create(ctx, request)
}
