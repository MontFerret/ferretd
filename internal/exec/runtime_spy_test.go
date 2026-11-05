package exec

import (
	"context"
	"errors"
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
		paramsErr           error
		compilePlan         api.Plan
		compileSources      []api.Source
		compileDebugSources []api.Source
		parameters          []string
		collect             func(context.Context, sessionOptionsSpy) (*api.Content, error)
		runFn               func(context.Context) (api.Output, error)
		paramsFn            func(context.Context) ([]string, error)

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

func (r *runtimeSpy) Version(ctx context.Context) (api.Version, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return "test", nil
}

func (r *runtimeSpy) Run(
	context.Context,
	api.Source,
	...api.SessionOption,
) (api.Output, error) {
	return nil, errors.New("not implemented")
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
