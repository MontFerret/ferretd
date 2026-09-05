package grpc

import (
	"context"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type executionService struct {
	executionv1.UnimplementedExecutionServiceServer
	executions *exec.Manager
}

func newExecutionService(executions *exec.Manager) (*executionService, error) {
	if executions == nil {
		return nil, errNilExecutionManager
	}

	return &executionService{executions: executions}, nil
}

func (s *executionService) CreateSession(
	ctx context.Context,
	request *executionv1.CreateSessionRequest,
) (*executionv1.CreateSessionResponse, error) {
	if request == nil || request.WorkspaceId == nil || request.WorkspaceId.Value == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace ID is required")
	}

	if request.RelativePath == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace-relative source path is required")
	}

	result, err := s.executions.CreateSession(
		ctx,
		workspace.ID(request.WorkspaceId.Value),
		request.RelativePath,
	)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.CreateSessionResponse{Session: toProtoSession(result)}, nil
}

func (s *executionService) GetSession(
	ctx context.Context,
	request *executionv1.GetSessionRequest,
) (*executionv1.GetSessionResponse, error) {
	if request == nil || request.Id == nil || request.Id.Value == "" {
		return nil, status.Error(codes.NotFound, "session not found")
	}

	result, err := s.executions.GetSession(ctx, exec.SessionID(request.Id.Value))
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.GetSessionResponse{Session: toProtoSession(result)}, nil
}

func (s *executionService) CloseSession(
	ctx context.Context,
	request *executionv1.CloseSessionRequest,
) (*executionv1.CloseSessionResponse, error) {
	if request == nil || request.Id == nil {
		return &executionv1.CloseSessionResponse{}, nil
	}

	if err := s.executions.CloseSession(ctx, exec.SessionID(request.Id.Value)); err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.CloseSessionResponse{}, nil
}

func (s *executionService) CreateExecution(
	ctx context.Context,
	request *executionv1.CreateExecutionRequest,
) (*executionv1.CreateExecutionResponse, error) {
	if request == nil || request.SessionId == nil || request.SessionId.Value == "" {
		return nil, status.Error(codes.InvalidArgument, "session ID is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, toExecutionStatusError(err)
	}

	parameters := map[string]any{}

	if request.Parameters != nil {
		parameters = request.Parameters.AsMap()
	}

	options, err := fromProtoExecutionOptions(request.Options)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	result, err := s.executions.CreateExecution(
		ctx,
		exec.SessionID(request.SessionId.Value),
		exec.Parameters(parameters),
		options,
	)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	encoded, err := toProtoExecution(result)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.CreateExecutionResponse{Execution: encoded}, nil
}

func (s *executionService) RunExecution(
	ctx context.Context,
	request *executionv1.RunExecutionRequest,
) (*executionv1.RunExecutionResponse, error) {
	if request == nil || request.Id == nil || request.Id.Value == "" {
		return nil, status.Error(codes.NotFound, "execution not found")
	}

	result, err := s.executions.RunExecution(ctx, exec.ExecutionID(request.Id.Value))
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	encoded, err := toProtoExecution(result)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.RunExecutionResponse{Execution: encoded}, nil
}

func (s *executionService) GetExecution(
	ctx context.Context,
	request *executionv1.GetExecutionRequest,
) (*executionv1.GetExecutionResponse, error) {
	if request == nil || request.Id == nil || request.Id.Value == "" {
		return nil, status.Error(codes.NotFound, "execution not found")
	}

	result, err := s.executions.GetExecution(ctx, exec.ExecutionID(request.Id.Value))
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	encoded, err := toProtoExecution(result)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.GetExecutionResponse{Execution: encoded}, nil
}

func (s *executionService) CancelExecution(
	ctx context.Context,
	request *executionv1.CancelExecutionRequest,
) (*executionv1.CancelExecutionResponse, error) {
	if request == nil || request.Id == nil || request.Id.Value == "" {
		return nil, status.Error(codes.NotFound, "execution not found")
	}

	result, err := s.executions.CancelExecution(ctx, exec.ExecutionID(request.Id.Value))
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	encoded, err := toProtoExecution(result)
	if err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.CancelExecutionResponse{Execution: encoded}, nil
}

func (s *executionService) CloseExecution(
	ctx context.Context,
	request *executionv1.CloseExecutionRequest,
) (*executionv1.CloseExecutionResponse, error) {
	if request == nil || request.Id == nil {
		return &executionv1.CloseExecutionResponse{}, nil
	}

	if err := s.executions.CloseExecution(ctx, exec.ExecutionID(request.Id.Value)); err != nil {
		return nil, toExecutionStatusError(err)
	}

	return &executionv1.CloseExecutionResponse{}, nil
}

func (s *executionService) WatchExecution(
	request *executionv1.WatchExecutionRequest,
	stream grpcgo.ServerStreamingServer[executionv1.WatchExecutionResponse],
) error {
	if request == nil || request.Id == nil || request.Id.Value == "" {
		return status.Error(codes.NotFound, "execution not found")
	}

	subscription, err := s.executions.WatchExecution(stream.Context(), exec.ExecutionID(request.Id.Value))
	if err != nil {
		return toExecutionStatusError(err)
	}

	defer subscription.Cancel()

	if err := sendExecutionEvent(stream, subscription.Current); err != nil {
		return err
	}

	if subscription.Current.Snapshot.State.Terminal() {
		return nil
	}

	events := subscription.Events
	errorsChannel := subscription.Errors

	for {
		select {
		case <-stream.Context().Done():
			return status.FromContextError(stream.Context().Err()).Err()
		case event, ok := <-events:
			if !ok {
				return subscriptionStatusError(errorsChannel)
			}

			if err := sendExecutionEvent(stream, event); err != nil {
				return err
			}

			if event.Snapshot.State.Terminal() {
				return nil
			}
		case watchErr, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil

				continue
			}

			if watchErr != nil {
				return toExecutionStatusError(watchErr)
			}
		}
	}
}
