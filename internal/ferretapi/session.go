package ferretapi

import (
	"context"
	"sync"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"
)

type session struct {
	session *ferret.Session
	source  api.Source

	closeOnce sync.Once
	closeErr  error
}

var _ api.Session = (*session)(nil)

func (s *session) Run(ctx context.Context) (api.Output, error) {
	output, err := s.session.Run(ctx)
	if output == nil {
		return api.Output{}, wrapDiagnosticError(s.source, err)
	}

	return api.Output{
		ContentType: output.ContentType,
		Content:     append([]byte(nil), output.Content...),
	}, wrapDiagnosticError(s.source, err)
}

func (s *session) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.session.Close()
	})

	return s.closeErr
}
