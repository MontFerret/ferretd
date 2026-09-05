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

	if err := d.requireInspectable(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	existing := append([]apidebugger.Breakpoint(nil), d.breakpoints[sourceName]...)
	d.mu.Unlock()

	for _, breakpoint := range existing {
		if err := d.runtime.Debugger().DeleteBreakpoint(breakpoint.ID); err != nil {
			return nil, err
		}
	}

	d.mu.Lock()
	d.breakpoints[sourceName] = nil
	d.mu.Unlock()

	bound := make([]apidebugger.Breakpoint, 0, len(locations))
	for _, location := range locations {
		breakpoint, err := d.runtime.Debugger().SetBreakpointAt(
			apisource.Location{
				SourceName: sourceName,
				Position:   location,
			},
			apidebugger.BreakpointOptions{
				BindingMode: apidebugger.BreakpointBindNextExecutableInSource,
			},
		)
		if err != nil {
			d.mu.Lock()
			d.breakpoints[sourceName] = bound
			d.mu.Unlock()

			return nil, err
		}

		bound = append(bound, breakpoint)
	}

	d.mu.Lock()
	d.breakpoints[sourceName] = bound
	d.mu.Unlock()

	return bound, nil
}
