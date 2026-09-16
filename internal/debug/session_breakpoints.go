package debug

import (
	"context"

	apidebugger "github.com/MontFerret/api/debugger"
)

// replaceBreakpoints replaces every breakpoint for one source.
func (d *session) replaceBreakpoints(
	ctx context.Context,
	sourceName string,
	requests []apidebugger.BreakpointRequest,
) ([]apidebugger.Breakpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := d.requireActive(); err != nil {
		return nil, err
	}

	return d.runtime.Debugger().ReplaceBreakpoints(ctx, sourceName, requests)
}
