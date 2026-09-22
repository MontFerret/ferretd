package workspace

import "context"

// Cancels when filesystem admission has registered the selected parent, before
// retained state is published. This avoids timing-dependent cancellation sleeps.
type watchCancellationContext struct {
	context.Context
	cancel    context.CancelFunc
	watcher   *workspaceWatcher
	directory string
}

func (c *watchCancellationContext) Err() error {
	if c.watcher.WatchesSubtree(c.directory) {
		c.cancel()
	}

	return c.Context.Err()
}
