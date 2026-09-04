package exec

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type planSpy struct {
	runtime *runtimeSpy
	source  api.Source
	debug   bool

	mu           sync.Mutex
	closed       bool
	closeOnce    sync.Once
	closeErr     error
	lastOptions  sessionOptionsSpy
	newSessionFn func(context.Context, ...api.SessionOption) (api.Session, error)
	newDebugFn   func(context.Context, ...api.SessionOption) (apidebugger.Session, error)
}

var _ api.Plan = (*planSpy)(nil)

func (p *planSpy) Params() []string {
	if strings.Contains(p.source.Content, "@value") {
		return []string{"value"}
	}

	return nil
}

func (p *planSpy) NewSession(
	ctx context.Context,
	options ...api.SessionOption,
) (api.Session, error) {
	if p.newSessionFn != nil {
		return p.newSessionFn(ctx, options...)
	}

	parsed, err := newSessionOptionsSpy(options)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("plan is closed")
	}
	p.lastOptions = parsed

	return &sessionSpy{runtime: p.runtime, source: p.source, options: parsed}, nil
}

func (p *planSpy) NewDebugSession(
	ctx context.Context,
	options ...api.SessionOption,
) (apidebugger.Session, error) {
	if p.newDebugFn != nil {
		return p.newDebugFn(ctx, options...)
	}

	parsed, err := newSessionOptionsSpy(options)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("plan is closed")
	}
	p.lastOptions = parsed

	return &debugSessionSpy{runtime: p.runtime}, nil
}

func (p *planSpy) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()

		if p.runtime.planClose != nil {
			p.closeErr = p.runtime.planClose()
		}
	})

	return p.closeErr
}
