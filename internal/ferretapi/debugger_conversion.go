package ferretapi

import (
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferret/v2"
)

func nativeBreakpointMode(value apidebugger.BreakpointBindingMode) ferret.DebugBreakpointBindingMode {
	switch value {
	case apidebugger.BreakpointBindExact:
		return ferret.DebugBreakpointBindExact
	case apidebugger.BreakpointBindNextExecutableInFunction:
		return ferret.DebugBreakpointBindNextExecutableInFunction
	default:
		return ferret.DebugBreakpointBindNextExecutableInFile
	}
}

func convertBreakpointMode(value ferret.DebugBreakpointBindingMode) apidebugger.BreakpointBindingMode {
	switch value {
	case ferret.DebugBreakpointBindExact:
		return apidebugger.BreakpointBindExact
	case ferret.DebugBreakpointBindNextExecutableInFunction:
		return apidebugger.BreakpointBindNextExecutableInFunction
	default:
		return apidebugger.BreakpointBindNextExecutableInSource
	}
}

func convertBreakpoint(value ferret.DebugBreakpoint) apidebugger.Breakpoint {
	return apidebugger.Breakpoint{
		Location:          convertRange(value.Location),
		RequestedLocation: convertLocation(value.RequestedLocation),
		ID:                apidebugger.BreakpointID(value.ID),
		PointID:           apidebugger.PointID(value.PointID),
		FunctionID:        apidebugger.FunctionID(value.FunctionID),
		BindingMode:       convertBreakpointMode(value.BindingMode),
		Bound:             value.Bound,
	}
}

func convertRange(value ferret.Range) apisource.Range {
	return apisource.Range{
		Location: convertLocation(value.Location),
		Span: apisource.Span{
			Start: value.Span.Start,
			End:   value.Span.End,
		},
	}
}

func convertLocation(value ferret.Location) apisource.Location {
	return apisource.Location{
		Position: apisource.Position{
			Line:   value.Line,
			Column: value.Column,
		},
		SourceName: value.File,
	}
}

func convertVariables(values []ferret.DebugVariable) []apidebugger.Variable {
	result := make([]apidebugger.Variable, len(values))
	for index := range values {
		result[index] = apidebugger.Variable{
			Name:    values[index].Name,
			Value:   convertValue(values[index].Value),
			Mutable: values[index].Mutable,
			Param:   values[index].Param,
		}
	}

	return result
}

func convertValue(value ferret.DebugValue) apidebugger.Value {
	return apidebugger.Value{
		Type:      value.Type,
		Display:   value.Display,
		Reference: apidebugger.ValueReference(value.Reference),
	}
}
