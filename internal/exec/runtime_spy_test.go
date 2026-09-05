package exec

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/api"
)

type (
	runtimeSpyOption func(*runtimeSpy)

	runtimeSpy struct {
		mu sync.Mutex

		beforeCompile       func(context.Context) error
		beforeRun           func(context.Context) (context.Context, error)
		sessionClose        func() error
		planClose           func() error
		compileErr          error
		compilePlan         api.Plan
		compileSources      []api.Source
		compileDebugSources []api.Source
		parameters          []string
		run                 func(context.Context, sessionOptionsSpy) (api.Output, error)

		compileCalls      atomic.Int64
		compileDebugCalls atomic.Int64
		closeCalls        atomic.Int64
	}
)

var _ api.Runtime = (*runtimeSpy)(nil)

func newRuntimeSpy(options ...runtimeSpyOption) *runtimeSpy {
	result := &runtimeSpy{}
	for _, option := range options {
		option(result)
	}

	return result
}

func withBeforeCompileHook(hook func(context.Context) error) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.beforeCompile = hook
	}
}

func withBeforeRunHook(hook func(context.Context) (context.Context, error)) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.beforeRun = hook
	}
}

func withSessionCloseHook(hook func() error) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.sessionClose = hook
	}
}

func withPlanCloseHook(hook func() error) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.planClose = hook
	}
}

func (r *runtimeSpy) Run(
	ctx context.Context,
	source api.Source,
	options ...api.SessionOption,
) (api.Output, error) {
	plan, err := r.Compile(ctx, source)
	if err != nil {
		return api.Output{}, err
	}
	defer func() { _ = plan.Close() }()

	session, err := plan.NewSession(ctx, options...)
	if err != nil {
		return api.Output{}, err
	}
	defer func() { _ = session.Close() }()

	return session.Run(ctx)
}

func (r *runtimeSpy) Compile(
	ctx context.Context,
	source api.Source,
	_ ...api.PlanOption,
) (api.Plan, error) {
	r.compileCalls.Add(1)
	r.mu.Lock()
	r.compileSources = append(r.compileSources, source)
	compileErr := r.compileErr
	compilePlan := r.compilePlan
	r.mu.Unlock()
	if r.beforeCompile != nil {
		if err := r.beforeCompile(ctx); err != nil {
			return nil, err
		}
	}

	if compileErr != nil {
		return compilePlan, compileErr
	}

	return &planSpy{runtime: r}, nil
}

func (r *runtimeSpy) CompileDebug(
	ctx context.Context,
	source api.Source,
	_ ...api.PlanOption,
) (api.Plan, error) {
	r.compileDebugCalls.Add(1)
	r.mu.Lock()
	r.compileDebugSources = append(r.compileDebugSources, source)
	r.mu.Unlock()
	if r.beforeCompile != nil {
		if err := r.beforeCompile(ctx); err != nil {
			return nil, err
		}
	}

	return &planSpy{runtime: r}, nil
}

func (r *runtimeSpy) Close() error {
	r.closeCalls.Add(1)

	return nil
}

func (r *runtimeSpy) sources() ([]api.Source, []api.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]api.Source(nil), r.compileSources...),
		append([]api.Source(nil), r.compileDebugSources...)
}
