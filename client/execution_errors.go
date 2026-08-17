package client

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

var (
	errIncompleteExecutionSession = errors.New("daemon returned an incomplete execution session")
	errIncompleteExecution        = errors.New("daemon returned an incomplete execution")
	errIncompleteExecutionEvent   = errors.New("daemon returned an incomplete execution event")

	// ErrExecutionSourceNotFound reports a missing retained source document.
	ErrExecutionSourceNotFound = errors.New("execution source not found")
	// ErrExecutionSourceClosed reports a workspace that can no longer compile sources.
	ErrExecutionSourceClosed = errors.New("execution source is closed")
	// ErrSessionNotFound reports an unknown daemon Session.
	ErrSessionNotFound = errors.New("execution session not found")
	// ErrSessionClosed reports a Session that no longer accepts child Executions.
	ErrSessionClosed = errors.New("execution session is closed")
	// ErrExecutionNotFound reports an unknown daemon Execution.
	ErrExecutionNotFound = errors.New("execution not found")
	// ErrInvalidExecutionState reports an invalid lifecycle transition.
	ErrInvalidExecutionState = errors.New("invalid execution state")
	// ErrInvalidExecutionParameters reports parameter values rejected by the runtime boundary.
	ErrInvalidExecutionParameters = errors.New("invalid execution parameters")
	// ErrExecutionWatcherLagged reports a watcher disconnected after its buffer overflowed.
	ErrExecutionWatcherLagged = errors.New("execution watcher lagged")
	// ErrExecutionServiceClosed reports a daemon execution manager that is shutting down.
	ErrExecutionServiceClosed = errors.New("execution service is closed")
	// ErrCompilationFailed reports Ferret compiler diagnostics without creating a Session.
	ErrCompilationFailed = errors.New("execution compilation failed")
)

// CompilationError retains structured diagnostics for a failed CreateSession call.
type CompilationError struct {
	Source      SourceSnapshot
	Diagnostics []Diagnostic
	cause       error
}

func mapCreateSessionError(ctx context.Context, err error) error {
	mapped := mapError(ctx, err)
	grpcStatus, ok := status.FromError(mapped)
	if !ok {
		return mapped
	}

	if grpcStatus.Code() == codes.InvalidArgument {
		for _, detail := range grpcStatus.Details() {
			failure, ok := detail.(*executionv1.CompilationFailure)
			if !ok {
				continue
			}

			return compilationErrorFromProto(failure, mapped)
		}
	}

	if classified := executionResourceError(mapped); classified != nil {
		return classified
	}

	switch grpcStatus.Code() {
	case codes.NotFound:
		return fmt.Errorf("%w: %s", ErrExecutionSourceNotFound, grpcStatus.Message())
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: %s", ErrExecutionSourceClosed, grpcStatus.Message())
	default:
		return mapped
	}
}

func mapSessionError(ctx context.Context, err error) error {
	return mapExecutionStatus(ctx, err, ErrSessionNotFound, ErrSessionClosed)
}

func mapCreateExecutionError(ctx context.Context, err error) error {
	mapped := mapExecutionStatus(ctx, err, ErrSessionNotFound, ErrSessionClosed)
	grpcStatus, ok := status.FromError(mapped)
	if ok && grpcStatus.Code() == codes.InvalidArgument {
		return fmt.Errorf("%w: %s", ErrInvalidExecutionParameters, grpcStatus.Message())
	}

	return mapped
}

func mapExecutionError(ctx context.Context, err error) error {
	return mapExecutionStatus(ctx, err, ErrExecutionNotFound, ErrInvalidExecutionState)
}

func mapWatchExecutionError(ctx context.Context, err error) error {
	mapped := mapExecutionError(ctx, err)
	grpcStatus, ok := status.FromError(mapped)
	if ok && grpcStatus.Code() == codes.ResourceExhausted {
		return fmt.Errorf("%w: %s", ErrExecutionWatcherLagged, grpcStatus.Message())
	}

	return mapped
}

func mapExecutionStatus(ctx context.Context, err, notFound, failedPrecondition error) error {
	mapped := mapError(ctx, err)
	if classified := executionResourceError(mapped); classified != nil {
		return classified
	}

	grpcStatus, ok := status.FromError(mapped)
	if !ok {
		return mapped
	}

	switch grpcStatus.Code() {
	case codes.NotFound:
		return fmt.Errorf("%w: %s", notFound, grpcStatus.Message())
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: %s", failedPrecondition, grpcStatus.Message())
	default:
		return mapped
	}
}

func executionResourceError(err error) error {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		return nil
	}

	for _, value := range grpcStatus.Details() {
		detail, ok := value.(*executionv1.ResourceErrorDetail)
		if !ok {
			continue
		}

		classification := resourceClassification(detail.Resource, detail.Condition)
		if classification == nil {
			return nil
		}

		return fmt.Errorf("%w: %s", classification, grpcStatus.Message())
	}

	return nil
}

func resourceClassification(
	resource executionv1.ResourceKind,
	condition executionv1.ResourceCondition,
) error {
	switch {
	case resource == executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND:
		return ErrWorkspaceNotFound
	case resource == executionv1.ResourceKind_RESOURCE_KIND_SOURCE &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND:
		return ErrExecutionSourceNotFound
	case (resource == executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE ||
		resource == executionv1.ResourceKind_RESOURCE_KIND_SOURCE) &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED:
		return ErrExecutionSourceClosed
	case resource == executionv1.ResourceKind_RESOURCE_KIND_SESSION &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND:
		return ErrSessionNotFound
	case resource == executionv1.ResourceKind_RESOURCE_KIND_SESSION &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED:
		return ErrSessionClosed
	case resource == executionv1.ResourceKind_RESOURCE_KIND_EXECUTION &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND:
		return ErrExecutionNotFound
	case resource == executionv1.ResourceKind_RESOURCE_KIND_EXECUTION &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_STATE:
		return ErrInvalidExecutionState
	case resource == executionv1.ResourceKind_RESOURCE_KIND_EXECUTION &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_PARAMETERS:
		return ErrInvalidExecutionParameters
	case resource == executionv1.ResourceKind_RESOURCE_KIND_EXECUTION &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED:
		return ErrExecutionServiceClosed
	case resource == executionv1.ResourceKind_RESOURCE_KIND_WATCHER &&
		condition == executionv1.ResourceCondition_RESOURCE_CONDITION_LAGGED:
		return ErrExecutionWatcherLagged
	default:
		return nil
	}
}

func compilationErrorFromProto(value *executionv1.CompilationFailure, cause error) error {
	result := &CompilationError{cause: cause}
	if value != nil {
		result.Diagnostics = fromProtoDiagnostics(value.Diagnostics)
		if value.Source != nil {
			result.Source = SourceSnapshot{
				RelativePath: value.Source.RelativePath,
				URI:          value.Source.Uri,
				Revision:     value.Source.Revision,
			}
			if value.Source.WorkspaceId != nil {
				result.Source.WorkspaceID = value.Source.WorkspaceId.Value
			}
		}
	}

	return result
}

// Error describes the source that failed compilation.
func (e *CompilationError) Error() string {
	return fmt.Sprintf("%v: %s", ErrCompilationFailed, e.Source.RelativePath)
}

// Unwrap exposes both the stable classification and original RPC error.
func (e *CompilationError) Unwrap() []error {
	return []error{ErrCompilationFailed, e.cause}
}
