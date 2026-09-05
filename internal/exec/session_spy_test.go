package exec

import (
	"context"
	"sync"

	"github.com/MontFerret/api"
)

type sessionSpy struct {
	runtime *runtimeSpy
	options sessionOptionsSpy

	closeOnce sync.Once
	closeErr  error
}

var _ api.Session = (*sessionSpy)(nil)

func (s *sessionSpy) Run(ctx context.Context) (api.Output, error) {
	if s.runtime.beforeRun != nil {
		var err error

		ctx, err = s.runtime.beforeRun(ctx)
		if err != nil {
			return api.Output{}, err
		}
	}

	if s.runtime.run != nil {
		return s.runtime.run(ctx, s.options)
	}

	return api.Output{ContentType: s.options.contentType, Content: []byte("1")}, nil
}

func (s *sessionSpy) Close() error {
	s.closeOnce.Do(func() {
		if s.runtime.sessionClose != nil {
			s.closeErr = s.runtime.sessionClose()
		}
	})

	return s.closeErr
}
