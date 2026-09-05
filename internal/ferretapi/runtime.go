// Package ferretapi adapts the native Ferret runtime to the Universal Ferret API.
package ferretapi

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"
)

// Runtime adapts one native Ferret engine to api.Runtime. Its composition owner
// closes the Runtime rather than closing the wrapped engine independently.
type Runtime struct {
	engine *ferret.Engine

	closeOnce sync.Once
	closeErr  error
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
func (r *Runtime) Run(
	ctx context.Context,
	source api.Source,
	options ...api.SessionOption,
) (output api.Output, resultErr error) {
	compiled, err := r.Compile(ctx, source)
	if err != nil {
		return api.Output{}, err
	}

	defer func() {
		resultErr = errors.Join(resultErr, compiled.Close())
	}()

	created, err := compiled.NewSession(ctx, options...)
	if err != nil {
		return api.Output{}, err
	}

	defer func() {
		resultErr = errors.Join(resultErr, created.Close())
	}()

	return created.Run(ctx)
}

// Compile creates a reusable ordinary execution plan.
func (r *Runtime) Compile(
	ctx context.Context,
	source api.Source,
	options ...api.PlanOption,
) (api.Plan, error) {
	parsedOptions, err := newPlanOptions(options)
	if err != nil {
		return nil, err
	}

	if err := parsedOptions.validate(false); err != nil {
		return nil, err
	}

	compiled, err := r.engine.Compile(ctx, ferret.NewSource(source.Name, source.Content))
	if err != nil {
		if compiled != nil {
			err = errors.Join(err, compiled.Close())
		}

		return nil, wrapDiagnosticError(source, err)
	}

	if compiled == nil {
		return nil, errors.New("native runtime returned no plan")
	}

	return &plan{plan: compiled, source: source}, nil
}

// CompileDebug creates a reusable debugger-instrumented plan.
func (r *Runtime) CompileDebug(
	ctx context.Context,
	source api.Source,
	options ...api.PlanOption,
) (api.Plan, error) {
	parsedOptions, err := newPlanOptions(options)
	if err != nil {
		return nil, err
	}

	if err := parsedOptions.validate(true); err != nil {
		return nil, err
	}

	compiled, err := r.engine.CompileDebug(ctx, ferret.NewSource(source.Name, source.Content))
	if err != nil {
		if compiled != nil {
			err = errors.Join(err, compiled.Close())
		}

		return nil, wrapDiagnosticError(source, err)
	}

	if compiled == nil {
		return nil, errors.New("native runtime returned no debug plan")
	}

	return &plan{plan: compiled, source: source}, nil
}

// Close releases the owned native Ferret engine exactly once.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.engine.Close()
	})

	return r.closeErr
}
