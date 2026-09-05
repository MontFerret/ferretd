package debug

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

type (
	planSpy struct {
		runtime *runtimeSpy
		debug   bool
	}

	runtimeSessionOptions struct{}
)

var (
	_ api.Plan           = (*planSpy)(nil)
	_ api.SessionOptions = (*runtimeSessionOptions)(nil)
)

func (p *planSpy) Params() []string {
	return nil
}

func (p *planSpy) NewSession(
	context.Context,
	...api.SessionOption,
) (api.Session, error) {
	return nil, errors.New("not implemented")
}

func (p *planSpy) NewDebugSession(
	ctx context.Context,
	options ...api.SessionOption,
) (apidebugger.Session, error) {
	if !p.debug {
		return nil, errors.New("not a debug plan")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parsed := runtimeSessionOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(&parsed); err != nil {
			return nil, err
		}
	}

	return p.runtime.newDebugger(), nil
}

func (p *planSpy) Close() error {
	return nil
}

func (o *runtimeSessionOptions) SetParam(_ string, value any) error {
	if _, err := json.Marshal(value); err != nil {
		return err
	}

	return nil
}

func (o *runtimeSessionOptions) SetParams(parameters map[string]any) error {
	for name, value := range parameters {
		if err := o.SetParam(name, value); err != nil {
			return err
		}
	}

	return nil
}

func (o *runtimeSessionOptions) SetOutputContentType(string) error {
	return nil
}

func (o *runtimeSessionOptions) SetFSRoot(string) error {
	return nil
}
