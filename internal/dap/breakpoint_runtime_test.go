package dap

import (
	"context"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type (
	breakpointRuntime struct {
		api.Runtime
		debugger *breakpointDebugger
	}
	breakpointPlan struct {
		api.Plan
		debugger *breakpointDebugger
	}
	breakpointDebugger struct {
		apidebugger.Session
		continueFn func(context.Context) (*apidebugger.Event, error)
		replaceFn  func(context.Context, string, []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error)
	}
)

func (r *breakpointRuntime) CompileDebug(ctx context.Context, source api.Source, options ...api.PlanOption) (api.Plan, error) {
	plan, err := r.Runtime.CompileDebug(ctx, source, options...)
	if err != nil {
		return nil, err
	}

	return &breakpointPlan{Plan: plan, debugger: r.debugger}, nil
}

func (p *breakpointPlan) NewDebugSession(ctx context.Context, options ...api.SessionOption) (apidebugger.Session, error) {
	session, err := p.Plan.NewDebugSession(ctx, options...)
	if err != nil {
		return nil, err
	}

	p.debugger.Session = session

	return p.debugger, nil
}

func (d *breakpointDebugger) Continue(ctx context.Context) (*apidebugger.Event, error) {
	return d.continueFn(ctx)
}

func (d *breakpointDebugger) ReplaceBreakpoints(ctx context.Context, source string, requests []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
	if d.replaceFn != nil {
		return d.replaceFn(ctx, source, requests)
	}

	return d.Session.ReplaceBreakpoints(ctx, source, requests)
}
