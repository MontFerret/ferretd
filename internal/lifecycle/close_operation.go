// Package lifecycle provides small synchronization primitives for shared
// resource lifecycles.
package lifecycle

import (
	"context"
	"sync"
)

type (
	// CloseOperation coordinates one committed teardown operation. Its zero value
	// is ready for use. Exactly one Begin caller owns teardown, while every waiter
	// observes the result published by Finish. A CloseOperation must not be copied
	// after first use.
	CloseOperation struct {
		mu sync.Mutex

		state closeState
		done  chan struct{}
		err   error
	}

	closeState uint8
)

const (
	closeIdle closeState = iota
	closeStarted
	closeFinished
)

// Begin commits the close operation and reports whether the caller owns
// teardown.
func (c *CloseOperation) Begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != closeIdle {
		return false
	}

	c.state = closeStarted
	c.done = make(chan struct{})

	return true
}

// Started reports whether close has been committed.
func (c *CloseOperation) Started() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.state != closeIdle
}

// Finished reports whether teardown has published its result.
func (c *CloseOperation) Finished() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.state == closeFinished
}

// Finish publishes the teardown result. Only the Begin owner may call Finish,
// and it must do so exactly once.
func (c *CloseOperation) Finish(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != closeStarted {
		panic("lifecycle: finish close operation outside teardown")
	}

	c.err = err
	c.state = closeFinished
	close(c.done)
}

// Wait waits for teardown without transferring its ownership to the caller.
// Cancellation stops only this wait. Once Finish publishes a result, that
// result takes precedence over concurrent caller cancellation.
func (c *CloseOperation) Wait(ctx context.Context) error {
	if ctx == nil {
		panic("lifecycle: nil close wait context")
	}

	c.mu.Lock()
	switch c.state {
	case closeIdle:
		c.mu.Unlock()

		panic("lifecycle: wait before close operation begins")
	case closeFinished:
		err := c.err
		c.mu.Unlock()

		return err
	default:
		done := c.done
		c.mu.Unlock()

		select {
		case <-done:
			return c.result()
		case <-ctx.Done():
			c.mu.Lock()
			if c.state == closeFinished {
				err := c.err
				c.mu.Unlock()

				return err
			}

			c.mu.Unlock()

			return ctx.Err()
		}
	}
}

func (c *CloseOperation) result() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != closeFinished {
		panic("lifecycle: close result read before completion")
	}

	return c.err
}
