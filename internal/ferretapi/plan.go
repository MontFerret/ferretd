package ferretapi

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2"
)

type plan struct {
	plan   *ferret.Plan
	source api.Source

	closeOnce sync.Once
	closeErr  error
}

var _ api.Plan = (*plan)(nil)

func (p *plan) Params() []string {
	return p.plan.Params()
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
