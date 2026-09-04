package dap

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type (
	runtimeOwnershipSpy struct {
		mu         sync.Mutex
		events     []string
		closeCalls atomic.Int64
	}

	runtimeOwnershipPlan struct {
		runtime   *runtimeOwnershipSpy
		closeOnce sync.Once
	}
)

var (
	_ api.Runtime = (*runtimeOwnershipSpy)(nil)
	_ api.Plan    = (*runtimeOwnershipPlan)(nil)
)

func newRuntimeOwnershipSpy() *runtimeOwnershipSpy {
	return &runtimeOwnershipSpy{}
}

func (r *runtimeOwnershipSpy) Run(
	context.Context,
	api.Source,
	...api.SessionOption,
) (api.Output, error) {
	return api.Output{}, errors.New("not implemented")
}

func (r *runtimeOwnershipSpy) Compile(
	context.Context,
	api.Source,
	...api.PlanOption,
) (api.Plan, error) {
	return &runtimeOwnershipPlan{runtime: r}, nil
}

func (r *runtimeOwnershipSpy) CompileDebug(
	context.Context,
	api.Source,
	...api.PlanOption,
) (api.Plan, error) {
	return &runtimeOwnershipPlan{runtime: r}, nil
}

func (r *runtimeOwnershipSpy) Close() error {
	r.closeCalls.Add(1)
	r.record("runtime")

	return nil
}

func (r *runtimeOwnershipSpy) record(event string) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *runtimeOwnershipSpy) recordedEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.events...)
}

func (p *runtimeOwnershipPlan) Params() []string {
	return nil
}

func (p *runtimeOwnershipPlan) NewSession(
	context.Context,
	...api.SessionOption,
) (api.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *runtimeOwnershipPlan) NewDebugSession(
	context.Context,
	...api.SessionOption,
) (apidebugger.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *runtimeOwnershipPlan) Close() error {
	p.closeOnce.Do(func() { p.runtime.record("plan") })

	return nil
}
