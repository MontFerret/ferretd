package daemon

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type runtimeLifecyclePlan struct {
	runtime   *runtimeLifecycleSpy
	closeOnce sync.Once
}

var _ api.Plan = (*runtimeLifecyclePlan)(nil)

func (p *runtimeLifecyclePlan) Params() []string {
	return nil
}

func (p *runtimeLifecyclePlan) NewSession(
	context.Context,
	...api.SessionOption,
) (api.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *runtimeLifecyclePlan) NewDebugSession(
	context.Context,
	...api.SessionOption,
) (apidebugger.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *runtimeLifecyclePlan) Close() error {
	p.closeOnce.Do(func() { p.runtime.record("plan") })

	return nil
}
