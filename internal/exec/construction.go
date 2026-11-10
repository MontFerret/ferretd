package exec

import (
	"context"
	"errors"
)

// construction retains cancellation and rollback outcomes while its caller owns
// the temporary operation context. Only the constructor accesses cleanupErr;
// admission settlement transfers it under the parent lock. Cancellation may run
// concurrently and never requires that lock.
type construction struct {
	cancel     context.CancelCauseFunc
	cleanupErr error
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

func (c *construction) recordCleanup(err error) error {
	c.cleanupErr = errors.Join(c.cleanupErr, err)

	return err
}
