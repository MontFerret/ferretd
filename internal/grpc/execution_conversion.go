package grpc

import (
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func toProtoSession(value exec.SessionSnapshot) *executionv1.Session {
	return &executionv1.Session{
		Id:         &executionv1.SessionId{Value: value.ID.String()},
		Source:     toProtoSourceSnapshot(value.Source),
		Parameters: append([]string(nil), value.Parameters...),
	}
}

func toProtoSourceSnapshot(value workspace.SourceSnapshot) *executionv1.SourceSnapshot {
	return &executionv1.SourceSnapshot{
		WorkspaceId:  &workspacev1.WorkspaceId{Value: value.Workspace.String()},
		RelativePath: value.RelativePath,
		Uri:          value.URI.String(),
		Revision:     uint64(value.Revision),
	}
}

func toProtoExecution(value exec.ExecutionSnapshot) (*executionv1.Execution, error) {
	parameters, err := structpb.NewStruct(value.Parameters)
	if err != nil {
		return nil, fmt.Errorf("encode execution parameters: %w", err)
	}

	options := &executionv1.ExecutionOptions{
		OutputContentType: value.Options.OutputContentType,
	}

	if value.Options.WorkingDirectory != "" {
		workingDirectory := value.Options.WorkingDirectory
		options.WorkingDirectory = &workingDirectory
	}

	result := &executionv1.Execution{
		Id:         &executionv1.ExecutionId{Value: value.ID.String()},
		SessionId:  &executionv1.SessionId{Value: value.Session.String()},
		State:      toProtoExecutionState(value.State),
		Parameters: parameters,
		Options:    options,
	}

	if value.Output != nil {
		result.Output = &executionv1.Output{
			ContentType: value.Output.ContentType,
			Data:        append([]byte(nil), value.Output.Content...),
		}
	}

	if value.Failure != nil {
		result.Failure = toProtoFailure(value.Failure)
	}

	return result, nil
}

func toProtoExecutionState(value exec.State) executionv1.ExecutionState {
	switch value {
	case exec.StateCreated:
		return executionv1.ExecutionState_EXECUTION_STATE_CREATED
	case exec.StateRunning:
		return executionv1.ExecutionState_EXECUTION_STATE_RUNNING
	case exec.StateCompleted:
		return executionv1.ExecutionState_EXECUTION_STATE_COMPLETED
	case exec.StateFailed:
		return executionv1.ExecutionState_EXECUTION_STATE_FAILED
	case exec.StateCancelled:
		return executionv1.ExecutionState_EXECUTION_STATE_CANCELLED
	default:
		return executionv1.ExecutionState_EXECUTION_STATE_UNSPECIFIED
	}
}

func toProtoFailure(value *exec.Failure) *executionv1.Failure {
	if value == nil {
		return nil
	}

	result := &executionv1.Failure{
		Category:    toProtoFailureCategory(value.Category),
		Message:     value.Message,
		Diagnostics: make([]*executionv1.Diagnostic, 0, len(value.Diagnostics)),
	}

	for _, item := range value.Diagnostics {
		result.Diagnostics = append(result.Diagnostics, toProtoDiagnostic(item))
	}

	return result
}

func toProtoFailureCategory(value exec.FailureCategory) executionv1.FailureCategory {
	switch value {
	case exec.FailureSessionCreation:
		return executionv1.FailureCategory_FAILURE_CATEGORY_SESSION_CREATION
	case exec.FailureRuntime:
		return executionv1.FailureCategory_FAILURE_CATEGORY_RUNTIME
	case exec.FailureCleanup:
		return executionv1.FailureCategory_FAILURE_CATEGORY_CLEANUP
	default:
		return executionv1.FailureCategory_FAILURE_CATEGORY_UNSPECIFIED
	}
}

func toProtoDiagnostics(values []diagnostic.Diagnostic) []*executionv1.Diagnostic {
	result := make([]*executionv1.Diagnostic, 0, len(values))

	for _, value := range values {
		result = append(result, toProtoDiagnostic(value))
	}

	return result
}

func toProtoDiagnostic(value diagnostic.Diagnostic) *executionv1.Diagnostic {
	result := &executionv1.Diagnostic{
		Uri:                value.URI.String(),
		Range:              toProtoRange(value.Range),
		Severity:           toProtoDiagnosticSeverity(value.Severity),
		Code:               value.Code,
		Source:             value.Source,
		Message:            value.Message,
		RelatedInformation: make([]*executionv1.RelatedInformation, 0, len(value.RelatedInformation)),
	}

	for _, related := range value.RelatedInformation {
		result.RelatedInformation = append(result.RelatedInformation, &executionv1.RelatedInformation{
			Uri:     related.URI.String(),
			Range:   toProtoRange(related.Range),
			Message: related.Message,
		})
	}

	return result
}

func toProtoDiagnosticSeverity(value diagnostic.Severity) executionv1.DiagnosticSeverity {
	if value == diagnostic.SeverityError {
		return executionv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_ERROR
	}

	return executionv1.DiagnosticSeverity_DIAGNOSTIC_SEVERITY_UNSPECIFIED
}

func toProtoRange(value source.Range) *executionv1.Range {
	return &executionv1.Range{
		Start: &executionv1.Position{Line: value.Start.Line, Character: value.Start.Character},
		End:   &executionv1.Position{Line: value.End.Line, Character: value.End.Character},
	}
}

func toProtoExecutionEvent(value exec.Event) (*executionv1.WatchExecutionResponse, error) {
	if value.Execution == "" || value.Snapshot.ID != value.Execution {
		return nil, fmt.Errorf("execution event identity mismatch")
	}

	snapshot, err := toProtoExecution(value.Snapshot)
	if err != nil {
		return nil, err
	}

	result := &executionv1.WatchExecutionResponse{
		ExecutionId: &executionv1.ExecutionId{Value: value.Execution.String()},
		Sequence:    value.Sequence,
	}

	switch value.Kind {
	case exec.EventCreated:
		result.Payload = &executionv1.WatchExecutionResponse_Created{
			Created: &executionv1.ExecutionCreated{Execution: snapshot},
		}
	case exec.EventStarted:
		result.Payload = &executionv1.WatchExecutionResponse_Started{
			Started: &executionv1.ExecutionStarted{Execution: snapshot},
		}
	case exec.EventCompleted:
		result.Payload = &executionv1.WatchExecutionResponse_Completed{
			Completed: &executionv1.ExecutionCompleted{Execution: snapshot},
		}
	case exec.EventFailed:
		result.Payload = &executionv1.WatchExecutionResponse_Failed{
			Failed: &executionv1.ExecutionFailed{Execution: snapshot},
		}
	case exec.EventCancelled:
		result.Payload = &executionv1.WatchExecutionResponse_Cancelled{
			Cancelled: &executionv1.ExecutionCancelled{Execution: snapshot},
		}
	default:
		return nil, fmt.Errorf("unknown execution event kind %d", value.Kind)
	}

	return result, nil
}
