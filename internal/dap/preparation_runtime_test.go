package dap

import (
	"context"
	"errors"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type (
	preparationRuntime struct {
		api.Runtime
		compileFn func(context.Context, api.Source, bool) (api.Plan, error)
	}

	preparationPlan struct {
		api.Plan
		createFn func(context.Context, ...api.SessionOption) (apidebugger.Session, error)
		closeFn  func() error
	}
)

func (r *preparationRuntime) Compile(ctx context.Context, source api.Source, _ ...api.PlanOption) (api.Plan, error) {
	return r.compileFn(ctx, source, false)
}

func (r *preparationRuntime) CompileDebug(ctx context.Context, source api.Source, _ ...api.PlanOption) (api.Plan, error) {
	return r.compileFn(ctx, source, true)
}

func (p *preparationPlan) NewDebugSession(ctx context.Context, options ...api.SessionOption) (apidebugger.Session, error) {
	if p.createFn != nil {
		return p.createFn(ctx, options...)
	}

	return p.Plan.NewDebugSession(ctx, options...)
}

func (p *preparationPlan) Close() error {
	err := p.Plan.Close()
	if p.closeFn != nil {
		err = errors.Join(err, p.closeFn())
	}

	return err
}
