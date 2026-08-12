package workspace

import (
	"context"
	"sync"
)

type observedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func newObservedDoneContext(ctx context.Context) *observedDoneContext {
	return &observedDoneContext{
		Context:  ctx,
		observed: make(chan struct{}),
	}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() {
		close(c.observed)
	})

	return c.Context.Done()
}
