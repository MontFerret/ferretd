package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func toExecutionStatusError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code := status.FromContextError(err).Code()
		switch {
		case errors.Is(err, runtimes.ErrRuntimeUnavailable):
			return resourceStatusError(code, err, executionv1.ResourceKind_RESOURCE_KIND_RUNTIME, executionv1.ResourceCondition_RESOURCE_CONDITION_UNAVAILABLE)
		case errors.Is(err, exec.ErrSessionClosed):
			return resourceStatusError(code, err, executionv1.ResourceKind_RESOURCE_KIND_SESSION, executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED)
		case errors.Is(err, workspace.ErrClosed):
			return resourceStatusError(code, err, executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE, executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED)
		default:
			return status.FromContextError(err).Err()
		}
	}

	var compilation *exec.CompilationError

	if errors.As(err, &compilation) {
		base := status.New(codes.InvalidArgument, err.Error())

		withDetails, detailErr := base.WithDetails(&executionv1.CompilationFailure{
			Source:      toProtoSourceSnapshot(compilation.Source),
			Diagnostics: toProtoDiagnostics(compilation.Diagnostics),
		})
		if detailErr != nil {
			return base.Err()
		}

		return withDetails.Err()
	}

	switch {
	case errors.Is(err, exec.ErrDuplicateAcquisition):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, runtimes.ErrRuntimeNotFound):
		return resourceStatusError(codes.NotFound, err, executionv1.ResourceKind_RESOURCE_KIND_RUNTIME, executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND)
	case errors.Is(err, runtimes.ErrRuntimeUnavailable):
		return resourceStatusError(codes.Unavailable, err, executionv1.ResourceKind_RESOURCE_KIND_RUNTIME, executionv1.ResourceCondition_RESOURCE_CONDITION_UNAVAILABLE)
	case errors.Is(err, workspace.ErrNotFound):
		return resourceStatusError(codes.NotFound, err, executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE, executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND)
	case errors.Is(err, workspace.ErrDocumentNotFound):
		return resourceStatusError(codes.NotFound, err, executionv1.ResourceKind_RESOURCE_KIND_SOURCE, executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND)
	case errors.Is(err, exec.ErrSessionNotFound):
		return resourceStatusError(codes.NotFound, err, executionv1.ResourceKind_RESOURCE_KIND_SESSION, executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND)
	case errors.Is(err, exec.ErrExecutionNotFound):
		return resourceStatusError(codes.NotFound, err, executionv1.ResourceKind_RESOURCE_KIND_EXECUTION, executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND)
	case errors.Is(err, exec.ErrInvalidExecutionOptions):
		return resourceStatusError(codes.InvalidArgument, err, executionv1.ResourceKind_RESOURCE_KIND_EXECUTION, executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_OPTIONS)
	case errors.Is(err, workspace.ErrDocumentUnavailable):
		return resourceStatusError(codes.FailedPrecondition, err, executionv1.ResourceKind_RESOURCE_KIND_SOURCE, executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED)
	case errors.Is(err, workspace.ErrClosed):
		return resourceStatusError(codes.FailedPrecondition, err, executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE, executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED)
	case errors.Is(err, exec.ErrClosed):
		return resourceStatusError(codes.FailedPrecondition, err, executionv1.ResourceKind_RESOURCE_KIND_EXECUTION, executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED)
	case errors.Is(err, exec.ErrSessionClosed):
		return resourceStatusError(codes.FailedPrecondition, err, executionv1.ResourceKind_RESOURCE_KIND_SESSION, executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED)
	case errors.Is(err, exec.ErrExecutionRunning), errors.Is(err, exec.ErrExecutionTerminal):
		return resourceStatusError(codes.FailedPrecondition, err, executionv1.ResourceKind_RESOURCE_KIND_EXECUTION, executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_STATE)
	case errors.Is(err, exec.ErrWatcherLagged):
		return resourceStatusError(codes.ResourceExhausted, err, executionv1.ResourceKind_RESOURCE_KIND_WATCHER, executionv1.ResourceCondition_RESOURCE_CONDITION_LAGGED)
	default:
		return status.Error(codes.Internal, "execution operation failed")
	}
}

func resourceStatusError(
	code codes.Code,
	err error,
	resource executionv1.ResourceKind,
	condition executionv1.ResourceCondition,
) error {
	base := status.New(code, err.Error())

	withDetails, detailErr := base.WithDetails(&executionv1.ResourceErrorDetail{
		Resource:  resource,
		Condition: condition,
	})
	if detailErr != nil {
		return base.Err()
	}

	return withDetails.Err()
}
