package exec

import (
	"context"
	"fmt"
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
