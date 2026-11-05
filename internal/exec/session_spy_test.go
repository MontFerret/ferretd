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
			return nil, err
		}
	}

	if s.runtime.runFn != nil {
		return s.runtime.runFn(ctx)
	}

	return &outputSpy{collectFn: func(ctx context.Context) (*api.Content, error) {
		if s.runtime.collect != nil {
			content, err := s.runtime.collect(ctx, s.options)

			return cloneContent(content), err
		}

		return &api.Content{Metadata: api.Metadata{ContentType: s.options.contentType}, Data: []byte("1")}, nil
	}}, nil
}

func (s *sessionSpy) Close() error {
	s.closeOnce.Do(func() {
		if s.runtime.sessionClose != nil {
			s.closeErr = s.runtime.sessionClose()
		}
	})

	return s.closeErr
}
