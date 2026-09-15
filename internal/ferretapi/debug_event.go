package ferretapi

import apidebugger "github.com/MontFerret/api/debugger"

func convertEvent(value *apidebugger.Event) *apidebugger.Event {
	if value == nil {
		return nil
	}

	result := *value
	result.Error = wrapDiagnosticError(value.Error)

	result.HitBreakpointIDs = append([]apidebugger.BreakpointID(nil), value.HitBreakpointIDs...)
	if value.Output != nil {
		output := convertOutput(value.Output)
		result.Output = &output
	}

	return &result
}
