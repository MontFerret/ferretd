package exec

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/MontFerret/api"
)

type sessionSpy struct {
	runtime *runtimeSpy
	source  api.Source
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

	if strings.Contains(s.source.Content, "WAITFOR FALSE") {
		<-ctx.Done()

		return api.Output{}, ctx.Err()
	}

	content := []byte("1")
	if strings.Contains(s.source.Content, "@value") {
		encoded, err := json.Marshal(s.options.params["value"])
		if err != nil {
			return api.Output{}, err
		}
		content = encoded
	}

	return api.Output{ContentType: s.options.contentType, Content: content}, nil
}

func (s *sessionSpy) Close() error {
	s.closeOnce.Do(func() {
		if s.runtime.sessionClose != nil {
			s.closeErr = s.runtime.sessionClose()
		}
	})

	return s.closeErr
}
