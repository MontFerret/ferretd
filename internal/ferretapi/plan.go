package ferretapi

import (
	"context"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2"
)

type plan struct{ plan *ferret.Plan }

var _ api.Plan = (*plan)(nil)

func (p *plan) Params() []string { return p.plan.Params() }

func (p *plan) NewSession(ctx context.Context, options ...api.SessionOption) (api.Session, error) {
	created, err := p.plan.NewSession(ctx, options...)
	if err != nil {
		return nil, wrapDiagnosticError(err)
	}

	return &session{session: created}, nil
}

func (p *plan) NewDebugSession(ctx context.Context, options ...api.SessionOption) (apidebugger.Session, error) {
	created, err := p.plan.NewDebugSession(ctx, options...)
	if err != nil {
		return nil, wrapDiagnosticError(err)
	}

	return &debugSession{session: created}, nil
}

func (p *plan) Close() error { return p.plan.Close() }
