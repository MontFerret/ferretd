package grpc

import (
	"context"
	"errors"

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

	parameters := map[string]any{}
	if request.Parameters != nil {
		parameters = request.Parameters.AsMap()
	}

	options := exec.ExecutionOptions{}
	if request.Options != nil {
		options.OutputContentType = request.Options.OutputContentType
	}

	result, err := s.executions.CreateExecution(
		ctx,
		exec.SessionID(request.SessionId.Value),
		parameters,
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

func sendExecutionEvent(
	stream grpcgo.ServerStreamingServer[executionv1.WatchExecutionResponse],
	event exec.Event,
) error {
	encoded, err := toProtoExecutionEvent(event)
	if err != nil {
		return toExecutionStatusError(err)
	}

	return stream.Send(encoded)
}

func subscriptionStatusError(errorsChannel <-chan error) error {
	for watchErr := range errorsChannel {
		if watchErr != nil {
			return toExecutionStatusError(watchErr)
		}
	}

	return nil
}

func toExecutionStatusError(err error) error {
	var compilation *exec.CompilationError

	if errors.As(err, &compilation) {
		base := status.New(codes.InvalidArgument, exec.ErrCompilationFailed.Error())
		withDetails, detailErr := base.WithDetails(&executionv1.CompilationFailure{
			Source:      toProtoSourceSnapshot(compilation.Source),
			Diagnostics: toProtoDiagnostics(compilation.Diagnostics),
		})

		if detailErr != nil {
			return base.Err()
		}

		return withDetails.Err()
	}

	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return status.FromContextError(err).Err()
	case errors.Is(err, workspace.ErrNotFound):
		return resourceStatusError(
			codes.NotFound,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE,
			executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		)
	case errors.Is(err, workspace.ErrDocumentNotFound):
		return resourceStatusError(
			codes.NotFound,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_SOURCE,
			executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		)
	case errors.Is(err, exec.ErrSessionNotFound):
		return resourceStatusError(
			codes.NotFound,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_SESSION,
			executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		)
	case errors.Is(err, exec.ErrExecutionNotFound):
		return resourceStatusError(
			codes.NotFound,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		)
	case errors.Is(err, exec.ErrInvalidParameters):
		return resourceStatusError(
			codes.InvalidArgument,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_PARAMETERS,
		)
	case errors.Is(err, workspace.ErrDocumentUnavailable):
		return resourceStatusError(
			codes.FailedPrecondition,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_SOURCE,
			executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED,
		)
	case errors.Is(err, workspace.ErrClosed), errors.Is(err, exec.ErrWorkspaceClosed):
		return resourceStatusError(
			codes.FailedPrecondition,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE,
			executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED,
		)
	case errors.Is(err, exec.ErrManagerClosed):
		return resourceStatusError(
			codes.FailedPrecondition,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED,
		)
	case errors.Is(err, exec.ErrSessionClosed):
		return resourceStatusError(
			codes.FailedPrecondition,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_SESSION,
			executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED,
		)
	case errors.Is(err, exec.ErrExecutionRunning), errors.Is(err, exec.ErrExecutionTerminal):
		return resourceStatusError(
			codes.FailedPrecondition,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_STATE,
		)
	case errors.Is(err, exec.ErrWatcherLagged):
		return resourceStatusError(
			codes.ResourceExhausted,
			err,
			executionv1.ResourceKind_RESOURCE_KIND_WATCHER,
			executionv1.ResourceCondition_RESOURCE_CONDITION_LAGGED,
		)
	default:
		return status.Error(codes.Internal, "execution operation failed")
	}
}

func resourceStatusError(
	code codes.Code,
	err error,
	resource executionv1.ResourceKind,
	condition executionv1.ResourceCondition,
) error {
	base := status.New(code, err.Error())
	withDetails, detailErr := base.WithDetails(&executionv1.ResourceErrorDetail{
		Resource:  resource,
		Condition: condition,
	})

	if detailErr != nil {
		return base.Err()
	}

	return withDetails.Err()
}
