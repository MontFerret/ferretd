package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/api"
)

type runtimeLifecycleSpy struct {
	mu     sync.Mutex
	events []string

	closeCalls   atomic.Int64
	closeStarted chan struct{}
	releaseClose <-chan struct{}
	startOnce    sync.Once
}

var _ api.Runtime = (*runtimeLifecycleSpy)(nil)

func newRuntimeLifecycleSpy() *runtimeLifecycleSpy {
	return &runtimeLifecycleSpy{closeStarted: make(chan struct{})}
}

func (r *runtimeLifecycleSpy) Run(
	context.Context,
	api.Source,
	...api.SessionOption,
) (api.Output, error) {
	return api.Output{}, errors.New("not implemented")
}

func (r *runtimeLifecycleSpy) Compile(
	context.Context,
	api.Source,
	...api.PlanOption,
) (api.Plan, error) {
	return &runtimeLifecyclePlan{runtime: r}, nil
}

func (r *runtimeLifecycleSpy) CompileDebug(
	context.Context,
	api.Source,
	...api.PlanOption,
) (api.Plan, error) {
	return &runtimeLifecyclePlan{runtime: r}, nil
}

func (r *runtimeLifecycleSpy) Close() error {
	r.closeCalls.Add(1)
	r.record("runtime")
	r.startOnce.Do(func() { close(r.closeStarted) })
	if r.releaseClose != nil {
		<-r.releaseClose
	}

	return nil
}

func (r *runtimeLifecycleSpy) record(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *runtimeLifecycleSpy) recordedEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.events...)
}
