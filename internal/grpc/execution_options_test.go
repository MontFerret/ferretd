package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestExecutionOptionsProtoRoundTrip(t *testing.T) {
	empty := ""
	blank := " \t\n "
	workingDirectory := "/runtime root"
	tests := []struct {
		name            string
		value           *executionv1.ExecutionOptions
		wantDirectory   string
		wantPresent     bool
		wantContentType string
	}{
		{name: "options omitted"},
		{name: "field omitted", value: &executionv1.ExecutionOptions{}},
		{
			name:        "empty field present",
			value:       &executionv1.ExecutionOptions{WorkingDirectory: &empty},
			wantPresent: true,
		},
		{
			name:          "whitespace field present",
			value:         &executionv1.ExecutionOptions{WorkingDirectory: &blank},
			wantDirectory: blank,
			wantPresent:   true,
		},
		{
			name: "field present",
			value: &executionv1.ExecutionOptions{
				OutputContentType: "application/msgpack",
				WorkingDirectory:  &workingDirectory,
			},
			wantDirectory:   workingDirectory,
			wantPresent:     true,
			wantContentType: "application/msgpack",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoded := fromProtoExecutionOptions(test.value)
			if present := decoded.WorkingDirectorySet; present != test.wantPresent {
				t.Fatalf("decoded working directory presence = %t, want %t", present, test.wantPresent)
			}
			if decoded.WorkingDirectory != test.wantDirectory {
				t.Fatalf("decoded WorkingDirectory = %q, want %q", decoded.WorkingDirectory, test.wantDirectory)
			}
			if decoded.OutputContentType != test.wantContentType {
				t.Fatalf("decoded OutputContentType = %q, want %q", decoded.OutputContentType, test.wantContentType)
			}

			encoded := toProtoExecutionOptions(decoded)
			if present := encoded.WorkingDirectory != nil; present != test.wantPresent {
				t.Fatalf("encoded working directory presence = %t, want %t", present, test.wantPresent)
			}
			if encoded.GetWorkingDirectory() != test.wantDirectory {
				t.Fatalf("encoded WorkingDirectory = %q, want %q", encoded.GetWorkingDirectory(), test.wantDirectory)
			}
			if encoded.OutputContentType != test.wantContentType {
				t.Fatalf("encoded OutputContentType = %q, want %q", encoded.OutputContentType, test.wantContentType)
			}
		})
	}
}

func TestExecutionServiceRejectsPresentBlankWorkingDirectory(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "empty"},
		{name: "whitespace", value: " \t\n "},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspaces := workspace.New()
			service, err := newExecutionService(mustNewExecutionManager(t, workspaces))
			if err != nil {
				t.Fatalf("newExecutionService: %v", err)
			}

			_, err = service.CreateExecution(context.Background(), &executionv1.CreateExecutionRequest{
				SessionId: &executionv1.SessionId{Value: "session"},
				Options:   &executionv1.ExecutionOptions{WorkingDirectory: &test.value},
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
		})
	}
}

func TestProtoExecutionWorkingDirectoryPresence(t *testing.T) {
	tests := []struct {
		name                string
		workingDirectory    string
		workingDirectorySet bool
		wantPresent         bool
		wantDirectory       string
	}{
		{name: "omitted"},
		{name: "unset value", workingDirectory: "/ignored"},
		{name: "empty present", workingDirectorySet: true, wantPresent: true},
		{
			name:                "present",
			workingDirectory:    "/canonical/runtime",
			workingDirectorySet: true,
			wantPresent:         true,
			wantDirectory:       "/canonical/runtime",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := toProtoExecution(exec.ExecutionSnapshot{
				ID:      "execution",
				Session: "session",
				State:   exec.StateCreated,
				Options: exec.RuntimeOptions{
					WorkingDirectory:    test.workingDirectory,
					WorkingDirectorySet: test.workingDirectorySet,
				},
			})
			if err != nil {
				t.Fatalf("toProtoExecution: %v", err)
			}
			if present := encoded.Options.WorkingDirectory != nil; present != test.wantPresent {
				t.Fatalf("working_directory presence = %t, want %t", present, test.wantPresent)
			}
			if encoded.Options.GetWorkingDirectory() != test.wantDirectory {
				t.Fatalf(
					"WorkingDirectory = %q, want %q",
					encoded.Options.GetWorkingDirectory(),
					test.wantDirectory,
				)
			}
		})
	}
}
