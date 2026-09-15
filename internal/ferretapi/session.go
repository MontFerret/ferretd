package ferretapi

import (
	"context"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"
)

type session struct{ session *ferret.Session }

var _ api.Session = (*session)(nil)

func (s *session) Run(ctx context.Context) (api.Output, error) {
	output, err := s.session.Run(ctx)

	return convertOutput(output), wrapDiagnosticError(err)
}

func (s *session) Close() error { return s.session.Close() }
