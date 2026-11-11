package exec

import (
	"context"
	"errors"
)

// construction retains cancellation and rollback outcomes while its caller owns
// the temporary operation context. Only the constructor accesses cleanupErr;
// admission settlement transfers it under the parent lock. Cancellation may run
// concurrently and never requires that lock.
type (
	construction struct {
		cancel     context.CancelCauseFunc
		cleanupErr error
	}

	// PreparationCleanupError identifies a failed rollback separately from the
	// preparation failure, so composition can retain it in its close result.
	PreparationCleanupError struct {
		Cause error
	}
)

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
	if err == nil {
		return nil
	}

	cleanup := &PreparationCleanupError{Cause: err}
	c.cleanupErr = errors.Join(c.cleanupErr, cleanup)

	return cleanup
}

func (e *PreparationCleanupError) Error() string {
	return e.Cause.Error()
}

func (e *PreparationCleanupError) Unwrap() error {
	return e.Cause
}
