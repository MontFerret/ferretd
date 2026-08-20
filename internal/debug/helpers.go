package debug

import (
	"context"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/diagnostic"
)

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	return ctx.Err()
}

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

func cloneFailure(value *Failure) *Failure {
	if value == nil {
		return nil
	}

	result := &Failure{
		Message:     value.Message,
		Diagnostics: make([]diagnostic.Diagnostic, len(value.Diagnostics)),
	}
	for index, item := range value.Diagnostics {
		result.Diagnostics[index] = item
		result.Diagnostics[index].RelatedInformation = append(
			[]diagnostic.RelatedInformation(nil),
			item.RelatedInformation...,
		)
	}

	return result
}

func cloneSnapshot(value SessionSnapshot) SessionSnapshot {
	return SessionSnapshot{
		ID:               value.ID,
		Session:          value.Session,
		State:            value.State,
		Reason:           value.Reason,
		Location:         value.Location,
		HitBreakpointIDs: append([]uint64(nil), value.HitBreakpointIDs...),
		Parameters:       cloneParameters(value.Parameters),
		Options:          value.Options,
		Output:           cloneOutput(value.Output),
		Failure:          cloneFailure(value.Failure),
	}
}

func cloneEvent(value Event) Event {
	return Event{
		Session:  value.Session,
		Sequence: value.Sequence,
		Kind:     value.Kind,
		Snapshot: cloneSnapshot(value.Snapshot),
	}
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

func cloneOutput(value *Output) *Output {
	if value == nil {
		return nil
	}

	return &Output{
		ContentType: value.ContentType,
		Content:     append([]byte(nil), value.Content...),
	}
}
