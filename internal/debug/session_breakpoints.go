package debug

import (
	"context"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

// replaceBreakpoints replaces every breakpoint for one source.
func (d *session) replaceBreakpoints(
	ctx context.Context,
	sourceName string,
	locations []apisource.Position,
) ([]apidebugger.Breakpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireActive(); err != nil {
		return nil, err
	}

	requests := make([]apidebugger.BreakpointRequest, len(locations))
	for index, location := range locations {
		requests[index] = apidebugger.BreakpointRequest{
			Position: location,
			Options: apidebugger.BreakpointOptions{
				BindingMode: apidebugger.BreakpointBindNextExecutableInSource,
			},
		}
	}

	return d.runtime.Debugger().ReplaceBreakpoints(ctx, sourceName, requests)
}
