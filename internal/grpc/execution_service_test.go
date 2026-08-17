package grpc

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestExecutionStatusErrorsCarryTypedResourceDetails(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		code      codes.Code
		resource  executionv1.ResourceKind
		condition executionv1.ResourceCondition
	}{
		{
			name:      "workspace not found",
			err:       workspace.ErrNotFound,
			code:      codes.NotFound,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		},
		{
			name:      "source not found",
			err:       workspace.ErrDocumentNotFound,
			code:      codes.NotFound,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_SOURCE,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		},
		{
			name:      "session not found",
			err:       exec.ErrSessionNotFound,
			code:      codes.NotFound,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_SESSION,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		},
		{
			name:      "invalid state",
			err:       exec.ErrExecutionTerminal,
			code:      codes.FailedPrecondition,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_STATE,
		},
		{
			name:      "watcher lag",
			err:       exec.ErrWatcherLagged,
			code:      codes.ResourceExhausted,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_WATCHER,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_LAGGED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := toExecutionStatusError(tt.err)
			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != tt.code {
				t.Fatalf("code = %v, want %v", grpcStatus.Code(), tt.code)
			}

			var got *executionv1.ResourceErrorDetail
			for _, value := range grpcStatus.Details() {
				detail, ok := value.(*executionv1.ResourceErrorDetail)
				if ok {
					got = detail

					break
				}
			}
			if got == nil || got.Resource != tt.resource || got.Condition != tt.condition {
				t.Fatalf("resource detail = %+v, want %v/%v", got, tt.resource, tt.condition)
			}
		})
	}
}
