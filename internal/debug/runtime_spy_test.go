package debug

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
)

type runtimeSpy struct {
	mu sync.Mutex

	debuggers []*debuggerSessionSpy
}

var _ api.Runtime = (*runtimeSpy)(nil)

func newRuntimeSpy() *runtimeSpy {
	return &runtimeSpy{}
}

func (r *runtimeSpy) Run(
	context.Context,
	api.Source,
	...api.SessionOption,
) (api.Output, error) {
	return api.Output{}, errors.New("not implemented")
}

func (r *runtimeSpy) Compile(
	ctx context.Context,
	_ api.Source,
	_ ...api.PlanOption,
) (api.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &planSpy{runtime: r}, nil
}

func (r *runtimeSpy) CompileDebug(
	ctx context.Context,
	_ api.Source,
	_ ...api.PlanOption,
) (api.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &planSpy{runtime: r, debug: true}, nil
}

func (r *runtimeSpy) Close() error {
	return nil
}

func (r *runtimeSpy) newDebugger() *debuggerSessionSpy {
	debugger := newDebuggerSessionSpy()

	r.mu.Lock()
	r.debuggers = append(r.debuggers, debugger)
	r.mu.Unlock()

	return debugger
}

func (r *runtimeSpy) latestDebugger() *debuggerSessionSpy {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.debuggers) == 0 {
		return nil
	}

	return r.debuggers[len(r.debuggers)-1]
}
