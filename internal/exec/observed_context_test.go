package exec

import (
	"context"
	"sync"
)

type observedDoneContext struct {
	context.Context

	once     sync.Once
	observed chan struct{}
}

func newObservedDoneContext() *observedDoneContext {
	return &observedDoneContext{
		Context:  context.Background(),
		observed: make(chan struct{}),
	}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })

	return c.Context.Done()
}
