package language

import "github.com/MontFerret/ferret/v2/pkg/runtime"

// Options configures a protocol-neutral language service.
type Options struct {
	Parameters runtime.Params
}

func (o Options) normalized() Options {
	o.Parameters = o.Parameters.Clone()

	return o
}
