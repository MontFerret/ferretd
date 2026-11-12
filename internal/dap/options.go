package dap

import (
	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

// Options configures a DAP server.
type Options struct {
	// RuntimeConfig uses the same definition schema as the daemon. DAP constructs
	// its own catalog and runtime rather than connecting to the daemon.
	RuntimeConfig runtimes.Config
	// Logger receives structured adapter diagnostics. Its writer must be safe for
	// concurrent use. A nil logger discards diagnostics.
	Logger *zerolog.Logger
}

func (o Options) normalized() Options {
	if o.Logger == nil {
		logger := zerolog.Nop()
		o.Logger = &logger
	}

	return o
}
