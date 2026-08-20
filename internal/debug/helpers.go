package debug

import "github.com/MontFerret/ferret/v2"

func convertLocation(value ferret.DebugLocation) Location {
	return Location{File: value.File, Line: value.Line, Column: value.Column}
}

func convertBreakpoint(value ferret.DebugBreakpoint) Breakpoint {
	return Breakpoint{
		ID:              uint64(value.ID),
		File:            value.File,
		RequestedLine:   value.RequestedLine,
		RequestedColumn: value.RequestedColumn,
		Line:            value.Line,
		Column:          value.Column,
		Verified:        value.Bound,
	}
}

func convertValue(value ferret.DebugValue) Value {
	return Value{
		Type:      value.Type,
		Display:   value.Display,
		Reference: ValueReference(value.Reference),
	}
}

func convertVariable(value ferret.DebugVariable) Variable {
	return Variable{
		Name:    value.Name,
		Value:   convertValue(value.Value),
		Mutable: value.Mutable,
	}
}

func convertVariables(values []ferret.DebugVariable) []Variable {
	result := make([]Variable, len(values))
	for index, value := range values {
		result[index] = convertVariable(value)
	}

	return result
}

func cloneParameters(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = cloneParameterValue(value)
	}

	return result
}

func cloneParameterValue(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = cloneParameterValue(typed[index])
		}

		return result
	case map[string]any:
		return cloneParameters(typed)
	default:
		return typed
	}
}
