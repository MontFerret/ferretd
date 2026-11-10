package client

import (
	"context"

	"google.golang.org/grpc"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

type executionClientStub struct {
	executionv1.ExecutionServiceClient
	create        func(context.Context, *executionv1.CreateExecutionRequest) (*executionv1.CreateExecutionResponse, error)
	createSession func(context.Context, *executionv1.CreateSessionRequest) (*executionv1.CreateSessionResponse, error)
	getSession    func(context.Context, *executionv1.GetSessionRequest) (*executionv1.GetSessionResponse, error)
	closeSession  func(context.Context, *executionv1.CloseSessionRequest) (*executionv1.CloseSessionResponse, error)
}

func (s *executionClientStub) CreateExecution(
	ctx context.Context,
	request *executionv1.CreateExecutionRequest,
	_ ...grpc.CallOption,
) (*executionv1.CreateExecutionResponse, error) {
	return s.create(ctx, request)
}

func (s *executionClientStub) CreateSession(ctx context.Context, request *executionv1.CreateSessionRequest, _ ...grpc.CallOption) (*executionv1.CreateSessionResponse, error) {
	return s.createSession(ctx, request)
}

func (s *executionClientStub) GetSession(ctx context.Context, request *executionv1.GetSessionRequest, _ ...grpc.CallOption) (*executionv1.GetSessionResponse, error) {
	return s.getSession(ctx, request)
}

func (s *executionClientStub) CloseSession(ctx context.Context, request *executionv1.CloseSessionRequest, _ ...grpc.CallOption) (*executionv1.CloseSessionResponse, error) {
	return s.closeSession(ctx, request)
}
