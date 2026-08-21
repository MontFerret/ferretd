package daemon

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/MontFerret/ferretd/internal/transport"
)

// Options configures a daemon instance.
type Options struct {
	Version  string
	Endpoint transport.Endpoint
	Logger   *slog.Logger
}

func (o Options) normalized() (Options, error) {
	if o.Endpoint == (transport.Endpoint{}) {
		endpoint, err := transport.DefaultEndpoint()
		if err != nil {
			return Options{}, fmt.Errorf("resolve daemon endpoint: %w", err)
		}

		o.Endpoint = endpoint
	}

	if o.Version == "" {
		o.Version = "dev"
	}

	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return o, nil
}
