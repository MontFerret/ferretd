// Package ferretapi adapts the native Ferret runtime to the Universal Ferret API.
package ferretapi

import (
	"context"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"
)

// Runtime adapts one native Ferret engine to api.Runtime. Its composition owner
// closes the Runtime rather than closing the wrapped engine independently.
type Runtime struct {
	engine *ferret.Engine
}

var _ api.Runtime = (*Runtime)(nil)

// New wraps a caller-constructed native Ferret engine and transfers responsibility
// for closing it to the returned Runtime. It panics when engine is nil.
func New(engine *ferret.Engine) *Runtime {
	if engine == nil {
		panic("ferretapi: nil native engine")
	}

	return &Runtime{engine: engine}
}

// Run compiles and executes source in a fresh session, releasing all transient resources.
func (r *Runtime) Run(ctx context.Context, source api.Source, options ...api.SessionOption) (api.Output, error) {
	output, err := r.engine.Run(ctx, ferret.NewSource(source.Name, source.Content), options...)

	return convertOutput(output), wrapDiagnosticError(err)
}

// Compile creates a reusable compiled plan using engine defaults or per-plan options.
func (r *Runtime) Compile(ctx context.Context, source api.Source, options ...api.PlanOption) (api.Plan, error) {
	compiled, err := r.engine.Compile(ctx, ferret.NewSource(source.Name, source.Content), options...)
	if err != nil {
		return nil, wrapDiagnosticError(err)
	}

	return &plan{plan: compiled}, nil
}

// CompileDebug creates a reusable plan carrying native debugger instrumentation.
func (r *Runtime) CompileDebug(ctx context.Context, source api.Source, options ...api.PlanOption) (api.Plan, error) {
	compiled, err := r.engine.CompileDebug(ctx, ferret.NewSource(source.Name, source.Content), options...)
	if err != nil {
		return nil, wrapDiagnosticError(err)
	}

	return &plan{plan: compiled}, nil
}

// Close releases the owned native engine after callers close their children.
func (r *Runtime) Close() error { return r.engine.Close() }
