package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestRuntimeOptionsFromProtoPreservesWorkingDirectoryPresence(t *testing.T) {
	workingDirectory := "/runtime root"
	tests := []struct {
		name            string
		value           *executionv1.ExecutionOptions
		wantDirectory   string
		wantContentType string
	}{
		{name: "options omitted"},
		{name: "field omitted", value: &executionv1.ExecutionOptions{}},
		{
			name: "field present",
			value: &executionv1.ExecutionOptions{
				OutputContentType: "application/msgpack",
				WorkingDirectory:  &workingDirectory,
			},
			wantDirectory:   workingDirectory,
			wantContentType: "application/msgpack",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := runtimeOptionsFromProto(test.value)
			if err != nil {
				t.Fatalf("runtimeOptionsFromProto: %v", err)
			}
			if got.WorkingDirectory != test.wantDirectory {
				t.Fatalf("WorkingDirectory = %q, want %q", got.WorkingDirectory, test.wantDirectory)
			}
			if got.OutputContentType != test.wantContentType {
				t.Fatalf("OutputContentType = %q, want %q", got.OutputContentType, test.wantContentType)
			}
		})
	}
}

func TestRuntimeOptionsFromProtoRejectsPresentBlankWorkingDirectory(t *testing.T) {
	for _, value := range []string{"", " \t\n "} {
		t.Run(value, func(t *testing.T) {
			_, err := runtimeOptionsFromProto(&executionv1.ExecutionOptions{WorkingDirectory: &value})
			if !errors.Is(err, exec.ErrInvalidExecutionOptions) {
				t.Fatalf("runtimeOptionsFromProto error = %v, want ErrInvalidExecutionOptions", err)
			}
		})
	}
}

func TestExecutionServiceRejectsPresentBlankWorkingDirectory(t *testing.T) {
	blank := " \t\n "
	workspaces := workspace.New()
	service, err := newExecutionService(mustNewExecutionManager(t, workspaces))
	if err != nil {
		t.Fatalf("newExecutionService: %v", err)
	}

	_, err = service.CreateExecution(context.Background(), &executionv1.CreateExecutionRequest{
		SessionId: &executionv1.SessionId{Value: "session"},
		Options:   &executionv1.ExecutionOptions{WorkingDirectory: &blank},
	})
	grpcStatus := status.Convert(err)
	if grpcStatus.Code() != codes.InvalidArgument {
		t.Fatalf("CreateExecution code = %v, want InvalidArgument", grpcStatus.Code())
	}

	var detail *executionv1.ResourceErrorDetail
	for _, value := range grpcStatus.Details() {
		if typed, ok := value.(*executionv1.ResourceErrorDetail); ok {
			detail = typed
			break
		}
	}
	if detail == nil || detail.Resource != executionv1.ResourceKind_RESOURCE_KIND_EXECUTION ||
		detail.Condition != executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_OPTIONS {
		t.Fatalf("resource detail = %+v, want execution/invalid-options", detail)
	}
}

func TestProtoExecutionWorkingDirectoryPresence(t *testing.T) {
	tests := []struct {
		name             string
		workingDirectory string
		wantPresent      bool
	}{
		{name: "omitted"},
		{name: "present", workingDirectory: "/canonical/runtime", wantPresent: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := toProtoExecution(exec.ExecutionSnapshot{
				ID:      "execution",
				Session: "session",
				State:   exec.StateCreated,
				Options: exec.RuntimeOptions{
					WorkingDirectory: test.workingDirectory,
				},
			})
			if err != nil {
				t.Fatalf("toProtoExecution: %v", err)
			}
			if present := encoded.Options.WorkingDirectory != nil; present != test.wantPresent {
				t.Fatalf("working_directory presence = %t, want %t", present, test.wantPresent)
			}
			if encoded.Options.GetWorkingDirectory() != test.workingDirectory {
				t.Fatalf(
					"WorkingDirectory = %q, want %q",
					encoded.Options.GetWorkingDirectory(),
					test.workingDirectory,
				)
			}
		})
	}
}
