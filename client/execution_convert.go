package client

import (
	"errors"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

func fromProtoSession(value *executionv1.Session) (Session, error) {
	if value == nil || value.Id == nil || value.Id.Value == "" || value.Source == nil ||
		value.Source.WorkspaceId == nil {

		return Session{}, errIncompleteExecutionSession
	}

	return Session{
		ID: SessionID(value.Id.Value),
		Source: SourceSnapshot{
			WorkspaceID:  value.Source.WorkspaceId.Value,
			RelativePath: value.Source.RelativePath,
			URI:          value.Source.Uri,
			Revision:     value.Source.Revision,
		},
		Parameters: append([]string(nil), value.Parameters...),
	}, nil
}

func fromProtoExecution(value *executionv1.Execution) (Execution, error) {
	if value == nil || value.Id == nil || value.Id.Value == "" || value.SessionId == nil ||
		value.SessionId.Value == "" {

		return Execution{}, errIncompleteExecution
	}

	state, err := fromProtoExecutionState(value.State)
	if err != nil {
		return Execution{}, err
	}

	result := Execution{
		ID:        ExecutionID(value.Id.Value),
		SessionID: SessionID(value.SessionId.Value),
		State:     state,
	}

	if value.Parameters != nil {
		result.Parameters = value.Parameters.AsMap()
	} else {
		result.Parameters = map[string]any{}
	}

	if value.Options != nil {
		result.Options.OutputContentType = value.Options.OutputContentType
	}

	if value.Output != nil {
		result.Output = &ExecutionOutput{
			ContentType: value.Output.ContentType,
			Data:        append([]byte(nil), value.Output.Data...),
		}
	}

	if value.Failure != nil {
		failure, err := fromProtoFailure(value.Failure)
		if err != nil {
			return Execution{}, err
		}

		result.Failure = &failure
	}

	return result, nil
}

func fromProtoExecutionState(value executionv1.ExecutionState) (ExecutionState, error) {
	switch value {
	case executionv1.ExecutionState_EXECUTION_STATE_CREATED:
		return ExecutionStateCreated, nil
	case executionv1.ExecutionState_EXECUTION_STATE_RUNNING:
		return ExecutionStateRunning, nil
	case executionv1.ExecutionState_EXECUTION_STATE_COMPLETED:
		return ExecutionStateCompleted, nil
	case executionv1.ExecutionState_EXECUTION_STATE_FAILED:
		return ExecutionStateFailed, nil
	case executionv1.ExecutionState_EXECUTION_STATE_CANCELLED:
		return ExecutionStateCancelled, nil
	default:
		return 0, errIncompleteExecution
	}
}

func fromProtoFailure(value *executionv1.Failure) (ExecutionFailure, error) {
	category, err := fromProtoFailureCategory(value.Category)
	if err != nil {
		return ExecutionFailure{}, err
	}

	return ExecutionFailure{
		Category:    category,
		Message:     value.Message,
		Diagnostics: fromProtoDiagnostics(value.Diagnostics),
	}, nil
}

func fromProtoFailureCategory(value executionv1.FailureCategory) (FailureCategory, error) {
	switch value {
	case executionv1.FailureCategory_FAILURE_CATEGORY_SESSION_CREATION:
		return FailureCategorySessionCreation, nil
	case executionv1.FailureCategory_FAILURE_CATEGORY_RUNTIME:
		return FailureCategoryRuntime, nil
	case executionv1.FailureCategory_FAILURE_CATEGORY_CLEANUP:
		return FailureCategoryCleanup, nil
	default:
		return 0, errIncompleteExecution
	}
}

func fromProtoDiagnostics(values []*executionv1.Diagnostic) []Diagnostic {
	result := make([]Diagnostic, 0, len(values))

	for _, value := range values {
		if value == nil {
			continue
		}

		item := Diagnostic{
			URI:      value.Uri,
			Range:    fromProtoRange(value.Range),
			Code:     value.Code,
			Source:   value.Source,
			Message:  value.Message,
			Severity: fromProtoDiagnosticSeverity(value.Severity),
		}

		for _, related := range value.RelatedInformation {
			if related == nil {
				continue
			}

			item.RelatedInformation = append(item.RelatedInformation, RelatedInformation{
				URI:     related.Uri,
				Range:   fromProtoRange(related.Range),
				Message: related.Message,
			})
		}

		result = append(result, item)
	}

	return result
}

func fromProtoDiagnosticSeverity(value executionv1.DiagnosticSeverity) DiagnosticSeverity {
	if value == executionv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR {
		return DiagnosticSeverityError
	}

	return 0
}

func fromProtoRange(value *executionv1.Range) Range {
	if value == nil {
		return Range{}
	}

	return Range{Start: fromProtoPosition(value.Start), End: fromProtoPosition(value.End)}
}

func fromProtoPosition(value *executionv1.Position) Position {
	if value == nil {
		return Position{}
	}

	return Position{Line: value.Line, Character: value.Character}
}

func fromProtoExecutionEvent(value *executionv1.WatchExecutionResponse) (ExecutionEvent, error) {
	if value == nil || value.ExecutionId == nil || value.ExecutionId.Value == "" || value.Sequence == 0 {
		return ExecutionEvent{}, errIncompleteExecutionEvent
	}

	kind, execution := protoEventPayload(value)
	if kind == 0 || execution == nil {
		return ExecutionEvent{}, errIncompleteExecutionEvent
	}

	snapshot, err := fromProtoExecution(execution)
	if err != nil {
		return ExecutionEvent{}, errors.Join(errIncompleteExecutionEvent, err)
	}
	eventID := ExecutionID(value.ExecutionId.Value)
	if snapshot.ID != eventID {
		return ExecutionEvent{}, errIncompleteExecutionEvent
	}

	return ExecutionEvent{
		ExecutionID: eventID,
		Sequence:    value.Sequence,
		Kind:        kind,
		Execution:   snapshot,
	}, nil
}

func protoEventPayload(value *executionv1.WatchExecutionResponse) (ExecutionEventKind, *executionv1.Execution) {
	switch payload := value.Payload.(type) {
	case *executionv1.WatchExecutionResponse_Created:
		return ExecutionEventCreated, payload.Created.GetExecution()
	case *executionv1.WatchExecutionResponse_Started:
		return ExecutionEventStarted, payload.Started.GetExecution()
	case *executionv1.WatchExecutionResponse_Completed:
		return ExecutionEventCompleted, payload.Completed.GetExecution()
	case *executionv1.WatchExecutionResponse_Failed:
		return ExecutionEventFailed, payload.Failed.GetExecution()
	case *executionv1.WatchExecutionResponse_Cancelled:
		return ExecutionEventCancelled, payload.Cancelled.GetExecution()
	default:
		return 0, nil
	}
}
