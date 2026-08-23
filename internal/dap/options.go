package dap

import (
	"github.com/rs/zerolog"
)

// Options configures a DAP server.
type Options struct {
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
