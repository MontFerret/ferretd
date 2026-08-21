package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

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
