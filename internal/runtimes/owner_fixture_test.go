package runtimes

import (
	"context"
	"sync/atomic"

	"github.com/MontFerret/api"
)

type rootSpy struct {
	api.Runtime
	closes  atomic.Int32
	onClose func() error
}

func (r *rootSpy) Close() error {
	r.closes.Add(1)

	if r.onClose != nil {
		return r.onClose()
	}

	return nil
}

func (r *rootSpy) Version(ctx context.Context) (api.Version, error) {
	return "test", ctx.Err()
}
