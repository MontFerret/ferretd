package client

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

func TestExecutionOptionsProtoRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		value       ExecutionOptions
		wantPresent bool
	}{
		{name: "zero"},
		{
			name:  "output content type",
			value: ExecutionOptions{OutputContentType: "application/msgpack"},
		},
		{
			name:        "working directory",
			value:       ExecutionOptions{WorkingDirectory: "/runtime root"},
			wantPresent: true,
		},
		{
			name:        "blank working directory",
			value:       ExecutionOptions{WorkingDirectory: " \t\n "},
			wantPresent: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := toProtoExecutionOptions(test.value)
			if present := encoded.WorkingDirectory != nil; present != test.wantPresent {
				t.Fatalf("working_directory presence = %t, want %t", present, test.wantPresent)
			}

			if got := fromProtoExecutionOptions(encoded); got != test.value {
				t.Fatalf("round trip = %+v, want %+v", got, test.value)
			}
		})
	}

	if got := fromProtoExecutionOptions(nil); got != (ExecutionOptions{}) {
		t.Fatalf("nil options = %+v, want zero options", got)
	}
}

func TestCreateExecutionWorkingDirectoryPresenceAndSnapshot(t *testing.T) {
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
			var captured *executionv1.CreateExecutionRequest
			stub := &executionClientStub{
				create: func(
					_ context.Context,
					request *executionv1.CreateExecutionRequest,
				) (*executionv1.CreateExecutionResponse, error) {
					captured = request
					return &executionv1.CreateExecutionResponse{Execution: &executionv1.Execution{
						Id:         &executionv1.ExecutionId{Value: "execution"},
						SessionId:  request.SessionId,
						State:      executionv1.ExecutionState_EXECUTION_STATE_CREATED,
						Parameters: &structpb.Struct{Fields: map[string]*structpb.Value{}},
						Options:    request.Options,
					}}, nil
				},
			}

			client := &ExecutionClient{client: stub}
			got, err := client.CreateExecution(context.Background(), CreateExecutionRequest{
				SessionID: "session",
				Options: ExecutionOptions{
					WorkingDirectory: test.workingDirectory,
				},
			})
			if err != nil {
				t.Fatalf("CreateExecution: %v", err)
			}
			if captured == nil || captured.Options == nil {
				t.Fatal("CreateExecution did not send execution options")
			}
			if present := captured.Options.WorkingDirectory != nil; present != test.wantPresent {
				t.Fatalf("working_directory presence = %t, want %t", present, test.wantPresent)
			}
			if captured.Options.GetWorkingDirectory() != test.workingDirectory {
				t.Fatalf(
					"request working directory = %q, want %q",
					captured.Options.GetWorkingDirectory(),
					test.workingDirectory,
				)
			}
			if got.Options.WorkingDirectory != test.workingDirectory {
				t.Fatalf(
					"snapshot working directory = %q, want %q",
					got.Options.WorkingDirectory,
					test.workingDirectory,
				)
			}
		})
	}
}

func TestCreateExecutionMapsInvalidOptionsClassification(t *testing.T) {
	grpcStatus := status.New(codes.InvalidArgument, "invalid execution options")
	withDetails, err := grpcStatus.WithDetails(&executionv1.ResourceErrorDetail{
		Resource:  executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
		Condition: executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_OPTIONS,
	})
	if err != nil {
		t.Fatalf("WithDetails: %v", err)
	}

	mapped := mapCreateExecutionError(context.Background(), withDetails.Err())
	if !errors.Is(mapped, ErrInvalidExecutionOptions) {
		t.Fatalf("mapCreateExecutionError = %v, want ErrInvalidExecutionOptions", mapped)
	}
	if errors.Is(mapped, ErrInvalidExecutionParameters) {
		t.Fatalf("mapCreateExecutionError = %v, did not want ErrInvalidExecutionParameters", mapped)
	}
}
