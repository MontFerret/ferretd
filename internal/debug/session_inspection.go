package debug

import (
	"context"

	apidebugger "github.com/MontFerret/api/debugger"
)

// frames returns the paused frame stack in current-to-caller order.
func (d *session) frames(ctx context.Context) ([]apidebugger.Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	return d.runtime.Debugger().Frames()
}

// scopes returns Locals and Parameters for one paused frame.
func (d *session) scopes(ctx context.Context, frame int) ([]Scope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	variables, err := d.runtime.Debugger().FrameLocals(frame)
	if err != nil {
		return nil, err
	}

	locals := Scope{Kind: ScopeLocals, Name: "Locals"}
	parameters := Scope{Kind: ScopeParameters, Name: "Parameters"}
	for _, variable := range variables {
		if variable.Param {
			parameters.Variables = append(parameters.Variables, variable)
		} else {
			locals.Variables = append(locals.Variables, variable)
		}
	}

	return []Scope{locals, parameters}, nil
}

// variables expands one value reference from the current paused state.
func (d *session) variables(
	ctx context.Context,
	reference apidebugger.ValueReference,
) ([]apidebugger.Variable, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	return d.runtime.Debugger().Variables(reference)
}

// evaluate evaluates a side-effect-free expression in one paused frame.
func (d *session) evaluate(
	ctx context.Context,
	frame int,
	expression string,
) (apidebugger.Value, error) {
	if err := ctx.Err(); err != nil {
		return apidebugger.Value{}, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return apidebugger.Value{}, err
	}

	value, err := d.runtime.Debugger().EvaluateFrame(ctx, frame, expression)
	if err != nil {
		return apidebugger.Value{}, err
	}

	return value, nil
}
