package exec

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/api"
)

// outputSpy exercises the owner's serialized Collect/Close path. Its collection
// callback transfers detached content and observes invocation cancellation.
type outputSpy struct {
	metadata  api.Metadata
	collectFn func(context.Context) (*api.Content, error)
	closeFn   func() error

	mu        sync.Mutex
	claimed   bool
	closed    bool
	closeOnce sync.Once
	closeErr  error
	collects  atomic.Int64
	closes    atomic.Int64
}

var _ api.Output = (*outputSpy)(nil)

func (o *outputSpy) Metadata() api.Metadata {
	return o.metadata
}

func (o *outputSpy) Collect(ctx context.Context) (*api.Content, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()

		return nil, api.ErrOutputClosed
	}

	if o.claimed {
		o.mu.Unlock()

		return nil, api.ErrOutputInUse
	}

	o.claimed = true
	o.mu.Unlock()
	o.collects.Add(1)

	var content *api.Content
	var err error

	if o.collectFn != nil {
		content, err = o.collectFn(ctx)
	}

	return content, errors.Join(err, o.Close())
}

func (o *outputSpy) Consume(context.Context, api.Consumer) error {
	return errors.New("unexpected output consumption")
}

func (o *outputSpy) Close() error {
	o.closeOnce.Do(func() {
		o.mu.Lock()
		o.closed = true
		o.mu.Unlock()
		o.closes.Add(1)

		if o.closeFn != nil {
			o.closeErr = o.closeFn()
		}
	})

	return o.closeErr
}
