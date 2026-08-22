package client

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
)

// ExecutionClient provides Session and Execution operations over a negotiated connection.
type ExecutionClient struct {
	client executionv1.ExecutionServiceClient
}

// CreateSession discovers or refreshes and compiles the latest saved contents
// of one eligible workspace document into an immutable Session.
func (c *ExecutionClient) CreateSession(ctx context.Context, request CreateSessionRequest) (Session, error) {
	response, err := c.client.CreateSession(ctx, &executionv1.CreateSessionRequest{
		WorkspaceId:  &workspacev1.WorkspaceId{Value: request.WorkspaceID},
		RelativePath: request.RelativePath,
	})
	if err != nil {
		return Session{}, mapCreateSessionError(ctx, err)
	}

	if response == nil {
		return Session{}, errIncompleteExecutionSession
	}

	return fromProtoSession(response.Session)
}

// GetSession returns one daemon Session by ID.
func (c *ExecutionClient) GetSession(ctx context.Context, id SessionID) (Session, error) {
	response, err := c.client.GetSession(ctx, &executionv1.GetSessionRequest{
		Id: &executionv1.SessionId{Value: string(id)},
	})
	if err != nil {
		return Session{}, mapSessionError(ctx, err)
	}

	if response == nil {
		return Session{}, errIncompleteExecutionSession
	}

	return fromProtoSession(response.Session)
}

// CloseSession idempotently closes a Session and all child Executions.
func (c *ExecutionClient) CloseSession(ctx context.Context, id SessionID) error {
	_, err := c.client.CloseSession(ctx, &executionv1.CloseSessionRequest{
		Id: &executionv1.SessionId{Value: string(id)},
	})

	return mapSessionError(ctx, err)
}

// CreateExecution creates one CREATED invocation with JSON-shaped parameters.
func (c *ExecutionClient) CreateExecution(
	ctx context.Context,
	request CreateExecutionRequest,
) (Execution, error) {
	parameters, err := structpb.NewStruct(request.Parameters)
	if err != nil {
		return Execution{}, fmt.Errorf("%w: %v", ErrInvalidExecutionParameters, err)
	}

	response, err := c.client.CreateExecution(ctx, &executionv1.CreateExecutionRequest{
		SessionId:  &executionv1.SessionId{Value: string(request.SessionID)},
		Parameters: parameters,
		Options: &executionv1.ExecutionOptions{
			OutputContentType: request.Options.OutputContentType,
		},
	})
	if err != nil {
		return Execution{}, mapCreateExecutionError(ctx, err)
	}

	if response == nil {
		return Execution{}, errIncompleteExecution
	}

	return fromProtoExecution(response.Execution)
}

// RunExecution starts exactly one invocation and returns its RUNNING snapshot.
func (c *ExecutionClient) RunExecution(ctx context.Context, id ExecutionID) (Execution, error) {
	response, err := c.client.RunExecution(ctx, &executionv1.RunExecutionRequest{
		Id: &executionv1.ExecutionId{Value: string(id)},
	})
	if err != nil {
		return Execution{}, mapExecutionError(ctx, err)
	}

	if response == nil {
		return Execution{}, errIncompleteExecution
	}

	return fromProtoExecution(response.Execution)
}

// GetExecution returns one retained Execution snapshot.
func (c *ExecutionClient) GetExecution(ctx context.Context, id ExecutionID) (Execution, error) {
	response, err := c.client.GetExecution(ctx, &executionv1.GetExecutionRequest{
		Id: &executionv1.ExecutionId{Value: string(id)},
	})
	if err != nil {
		return Execution{}, mapExecutionError(ctx, err)
	}

	if response == nil {
		return Execution{}, errIncompleteExecution
	}

	return fromProtoExecution(response.Execution)
}

// CancelExecution requests cancellation without overwriting terminal state.
func (c *ExecutionClient) CancelExecution(ctx context.Context, id ExecutionID) (Execution, error) {
	response, err := c.client.CancelExecution(ctx, &executionv1.CancelExecutionRequest{
		Id: &executionv1.ExecutionId{Value: string(id)},
	})
	if err != nil {
		return Execution{}, mapExecutionError(ctx, err)
	}

	if response == nil {
		return Execution{}, errIncompleteExecution
	}

	return fromProtoExecution(response.Execution)
}

// CloseExecution idempotently removes and settles an Execution.
func (c *ExecutionClient) CloseExecution(ctx context.Context, id ExecutionID) error {
	_, err := c.client.CloseExecution(ctx, &executionv1.CloseExecutionRequest{
		Id: &executionv1.ExecutionId{Value: string(id)},
	})

	return mapExecutionError(ctx, err)
}

// WatchExecution observes the latest lifecycle event and subsequent events.
func (c *ExecutionClient) WatchExecution(ctx context.Context, id ExecutionID) (*ExecutionWatcher, error) {
	stream, err := c.client.WatchExecution(ctx, &executionv1.WatchExecutionRequest{
		Id: &executionv1.ExecutionId{Value: string(id)},
	})
	if err != nil {
		return nil, mapWatchExecutionError(ctx, err)
	}

	return &ExecutionWatcher{ctx: ctx, stream: stream}, nil
}
