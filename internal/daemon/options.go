package daemon

import (
	"fmt"

	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/transport"
)

// Options configures a daemon instance.
type Options struct {
	Version  string
	Endpoint transport.Endpoint
	// Logger receives daemon diagnostics. Its writer must be safe for concurrent
	// use. A nil logger discards diagnostics.
	Logger *zerolog.Logger
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
		logger := zerolog.Nop()
		o.Logger = &logger
	}

	return o, nil
}
