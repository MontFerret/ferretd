package exec

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

func cloneEvent(value Event) Event {
	return Event{
		Execution: value.Execution,
		Sequence:  value.Sequence,
		Kind:      value.Kind,
		Snapshot:  cloneExecutionSnapshot(value.Snapshot),
	}
}

func invalidParametersError(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidParameters, err)
}

func newSessionID() (SessionID, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}

	return SessionID(value.String()), nil
}

func newExecutionID() (ExecutionID, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate execution ID: %w", err)
	}

	return ExecutionID(value.String()), nil
}

func waitForDone(ctx context.Context, done <-chan struct{}, result func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return result()
	}
}
