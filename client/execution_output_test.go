package client

import (
	"testing"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

func TestExecutionOutputPresence(t *testing.T) {
	for _, output := range []*executionv1.Output{nil, {}, {ContentType: "text/plain", Data: []byte("prefix")}} {
		converted, err := fromProtoExecution(&executionv1.Execution{
			Id:        &executionv1.ExecutionId{Value: "execution"},
			SessionId: &executionv1.SessionId{Value: "session"},
			State:     executionv1.ExecutionState_EXECUTION_STATE_COMPLETED,
			Output:    output,
		})
		if err != nil {
			t.Fatal(err)
		}

		if (converted.Output == nil) != (output == nil) {
			t.Fatalf("output presence = %+v, wire = %+v", converted.Output, output)
		}

		if output != nil && (converted.Output.ContentType != output.ContentType || string(converted.Output.Data) != string(output.Data)) {
			t.Fatalf("output = %+v, wire = %+v", converted.Output, output)
		}
	}
}
