package exec

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/diagnostic"
)

func newDebugSessionID() (DebugSessionID, error) {
	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate debug session ID: %w", err)
	}

	return DebugSessionID(value.String()), nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	return ctx.Err()
}

func convertDebugLocation(value ferret.DebugLocation) DebugLocation {
	return DebugLocation{File: value.File, Line: value.Line, Column: value.Column}
}

func convertDebugBreakpoint(value ferret.DebugBreakpoint) DebugBreakpoint {
	return DebugBreakpoint{
		ID:              uint64(value.ID),
		File:            value.File,
		RequestedLine:   value.RequestedLine,
		RequestedColumn: value.RequestedColumn,
		Line:            value.Line,
		Column:          value.Column,
		Verified:        value.Bound,
	}
}

func convertDebugValue(value ferret.DebugValue) DebugValue {
	return DebugValue{
		Type:      value.Type,
		Display:   value.Display,
		Reference: DebugValueReference(value.Reference),
	}
}

func convertDebugVariable(value ferret.DebugVariable) DebugVariable {
	return DebugVariable{
		Name:    value.Name,
		Value:   convertDebugValue(value.Value),
		Mutable: value.Mutable,
	}
}

func convertDebugVariables(values []ferret.DebugVariable) []DebugVariable {
	result := make([]DebugVariable, len(values))
	for index, value := range values {
		result[index] = convertDebugVariable(value)
	}

	return result
}

func cloneDebugFailure(value *DebugFailure) *DebugFailure {
	if value == nil {
		return nil
	}

	result := &DebugFailure{
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

func cloneDebugSnapshot(value DebugSessionSnapshot) DebugSessionSnapshot {
	return DebugSessionSnapshot{
		ID:               value.ID,
		Session:          value.Session,
		State:            value.State,
		Reason:           value.Reason,
		Location:         value.Location,
		HitBreakpointIDs: append([]uint64(nil), value.HitBreakpointIDs...),
		Parameters:       cloneParameters(value.Parameters),
		Options:          value.Options,
		Output:           cloneOutput(value.Output),
		Failure:          cloneDebugFailure(value.Failure),
	}
}

func cloneDebugEvent(value DebugEvent) DebugEvent {
	return DebugEvent{
		DebugSession: value.DebugSession,
		Sequence:     value.Sequence,
		Kind:         value.Kind,
		Snapshot:     cloneDebugSnapshot(value.Snapshot),
	}
}
