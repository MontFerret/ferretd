package grpc

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/api"
)

type (
	// These adapter tests exercise composition, framing, and validation.
	unusedRuntime struct{ t testing.TB }

	acquisitionRuntime struct {
		unusedRuntime
		releases atomic.Int32
	}

	// The transport test only compiles and closes plans.
	acquisitionPlan struct {
		api.Plan
		runtime *acquisitionRuntime
	}
)

var _ api.Runtime = (*unusedRuntime)(nil)

func (r *unusedRuntime) Version(context.Context) (api.Version, error) {
	r.t.Fatal("unexpected runtime Version")

	return "", nil
}

func (r *unusedRuntime) Run(context.Context, api.Source, ...api.SessionOption) (api.Output, error) {
	r.t.Fatal("unexpected runtime Run")

	return nil, nil
}

func (r *unusedRuntime) Compile(context.Context, api.Source, ...api.PlanOption) (api.Plan, error) {
	r.t.Fatal("unexpected runtime Compile")

	return nil, nil
}

func (r *unusedRuntime) CompileDebug(context.Context, api.Source, ...api.PlanOption) (api.Plan, error) {
	r.t.Fatal("unexpected runtime CompileDebug")

	return nil, nil
}

func (r *unusedRuntime) Close() error {
	return nil
}

func (r *acquisitionRuntime) Compile(context.Context, api.Source, ...api.PlanOption) (api.Plan, error) {
	return &acquisitionPlan{runtime: r}, nil
}

func (p *acquisitionPlan) Params(context.Context) ([]string, error) { return nil, nil }

func (p *acquisitionPlan) Close() error {
	p.runtime.releases.Add(1)

	return nil
}
