// Package ferretapi adapts the native Ferret runtime to the Universal Ferret API.
package ferretapi

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2"
)

type (
	// Runtime owns one native Ferret engine and exposes it through api.Runtime.
	Runtime struct {
		engine *ferret.Engine

		closeOnce sync.Once
		closeErr  error
	}

	plan struct {
		plan   *ferret.Plan
		source api.Source

		closeOnce sync.Once
		closeErr  error
	}

	session struct {
		session *ferret.Session
		source  api.Source

		closeOnce sync.Once
		closeErr  error
	}

	planOptions struct {
		optimizationLevel *api.OptimizationLevel
	}

	sessionOptions struct {
		options []ferret.SessionOption
	}
)

var (
	_ api.Runtime = (*Runtime)(nil)
	_ api.Plan    = (*plan)(nil)
	_ api.Session = (*session)(nil)
)

// New creates a Universal runtime backed by one native Ferret engine.
func New() (*Runtime, error) {
	engine, err := ferret.New()
	if err != nil {
		return nil, err
	}

	return &Runtime{engine: engine}, nil
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

	if err := parsedOptions.requireOptimizationLevel(api.OptimizationFull); err != nil {
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

	if err := parsedOptions.requireOptimizationLevel(api.OptimizationNone); err != nil {
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

func (p *plan) Params() []string {
	return append([]string(nil), p.plan.Params()...)
}

func (p *plan) NewSession(ctx context.Context, options ...api.SessionOption) (api.Session, error) {
	nativeOptions, err := newSessionOptions(options)
	if err != nil {
		return nil, err
	}

	created, err := p.plan.NewSession(ctx, nativeOptions.options...)
	if err != nil {
		if created != nil {
			err = errors.Join(err, created.Close())
		}

		return nil, wrapDiagnosticError(p.source, err)
	}

	if created == nil {
		return nil, errors.New("native runtime returned no session")
	}

	return &session{session: created, source: p.source}, nil
}

func (p *plan) NewDebugSession(
	ctx context.Context,
	options ...api.SessionOption,
) (apidebugger.Session, error) {
	nativeOptions, err := newSessionOptions(options)
	if err != nil {
		return nil, err
	}

	created, err := p.plan.NewDebugSession(ctx, nativeOptions.options...)
	if err != nil {
		if created != nil {
			err = errors.Join(err, created.Close())
		}

		return nil, wrapDiagnosticError(p.source, err)
	}

	if created == nil {
		return nil, errors.New("native runtime returned no debug session")
	}

	return newDebugSession(created, p.source), nil
}

func (p *plan) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.plan.Close()
	})

	return p.closeErr
}

func (s *session) Run(ctx context.Context) (api.Output, error) {
	output, err := s.session.Run(ctx)
	if output == nil {
		return api.Output{}, wrapDiagnosticError(s.source, err)
	}

	return api.Output{
		ContentType: output.ContentType,
		Content:     append([]byte(nil), output.Content...),
	}, wrapDiagnosticError(s.source, err)
}

func (s *session) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.session.Close()
	})

	return s.closeErr
}

func newPlanOptions(options []api.PlanOption) (*planOptions, error) {
	result := &planOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (o *planOptions) requireOptimizationLevel(supported api.OptimizationLevel) error {
	if o.optimizationLevel != nil && *o.optimizationLevel != supported {
		return fmt.Errorf(
			"optimization level %d is not supported by the provisional native runtime adapter",
			*o.optimizationLevel,
		)
	}

	return nil
}

func (o *planOptions) SetOptimizationLevel(level api.OptimizationLevel) error {
	o.optimizationLevel = &level

	return nil
}

func newSessionOptions(options []api.SessionOption) (*sessionOptions, error) {
	result := &sessionOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (o *sessionOptions) SetParam(name string, value any) error {
	o.options = append(o.options, ferret.WithSessionParam(name, value))

	return nil
}

func (o *sessionOptions) SetParams(parameters map[string]any) error {
	o.options = append(o.options, ferret.WithSessionParams(parameters))

	return nil
}

func (o *sessionOptions) SetOutputContentType(contentType string) error {
	o.options = append(o.options, ferret.WithOutputContentType(contentType))

	return nil
}

func (o *sessionOptions) SetFSRoot(root string) error {
	o.options = append(o.options, ferret.WithSessionFSRoot(root))

	return nil
}
