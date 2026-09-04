package debug

import (
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

func convertSourceLocation(value apisource.Location) Location {
	return Location{File: value.SourceName, Line: value.Line, Column: value.Column}
}

func convertRangeLocation(value apisource.Range) Location {
	return convertSourceLocation(value.Location)
}

func convertBreakpoint(value apidebugger.Breakpoint) Breakpoint {
	return Breakpoint{
		ID:              BreakpointID(value.ID),
		File:            value.RequestedLocation.SourceName,
		RequestedLine:   value.RequestedLocation.Line,
		RequestedColumn: value.RequestedLocation.Column,
		Line:            value.Location.Line,
		Column:          value.Location.Column,
		Verified:        value.Bound,
	}
}

func convertValue(value apidebugger.Value) Value {
	return Value{
		Type:      value.Type,
		Display:   value.Display,
		Reference: ValueReference(value.Reference),
	}
}

func convertVariable(value apidebugger.Variable) Variable {
	return Variable{
		Name:    value.Name,
		Value:   convertValue(value.Value),
		Mutable: value.Mutable,
	}
}

func convertVariables(values []apidebugger.Variable) []Variable {
	result := make([]Variable, len(values))
	for index, value := range values {
		result[index] = convertVariable(value)
	}

	return result
}
