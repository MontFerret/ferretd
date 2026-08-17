package client

import (
	"context"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

// ExecutionWatcher wraps one server-streaming execution observation.
type ExecutionWatcher struct {
	ctx    context.Context
	stream executionv1.ExecutionService_WatchExecutionClient
}

// Recv returns the next ordered event or io.EOF after terminal delivery.
func (w *ExecutionWatcher) Recv() (ExecutionEvent, error) {
	response, err := w.stream.Recv()
	if err != nil {
		return ExecutionEvent{}, mapWatchExecutionError(w.ctx, err)
	}

	return fromProtoExecutionEvent(response)
}
