package grpc

import (
	"context"
	"testing"

	"github.com/MontFerret/api"
)

// These adapter tests exercise composition, framing, and validation; execution
// through the concrete runtime is covered by daemon end-to-end tests.
type unusedRuntime struct {
	t testing.TB
}

var _ api.Runtime = (*unusedRuntime)(nil)

func (r *unusedRuntime) Run(context.Context, api.Source, ...api.SessionOption) (api.Output, error) {
	r.t.Fatal("unexpected runtime Run")

	return api.Output{}, nil
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
