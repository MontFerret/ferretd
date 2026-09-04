package dap

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type runtimeOwnershipPlan struct {
	runtime   *runtimeOwnershipSpy
	closeOnce sync.Once
}

var _ api.Plan = (*runtimeOwnershipPlan)(nil)

func (p *runtimeOwnershipPlan) Params() []string {
	return nil
}

func (p *runtimeOwnershipPlan) NewSession(
	context.Context,
	...api.SessionOption,
) (api.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *runtimeOwnershipPlan) NewDebugSession(
	context.Context,
	...api.SessionOption,
) (apidebugger.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *runtimeOwnershipPlan) Close() error {
	p.closeOnce.Do(func() { p.runtime.record("plan") })

	return nil
}
