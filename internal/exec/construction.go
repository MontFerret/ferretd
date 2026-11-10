package exec

import "context"

// construction retains only cancellation while its caller owns the temporary
// operation context. The parent lock protects membership, never cancellation.
type construction struct {
	cancel context.CancelCauseFunc
}

func newConstruction(ctx context.Context) (*construction, context.Context) {
	operation, cancel := context.WithCancelCause(ctx)

	return &construction{cancel: cancel}, operation
}

func (c *construction) finish() {
	c.cancel(nil)
}

func (c *construction) stop(cause error) {
	c.cancel(cause)
}
