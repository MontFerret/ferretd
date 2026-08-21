package exec

import (
	"reflect"
	"testing"

	"github.com/MontFerret/ferretd/internal/diagnostic"
)

func TestExecutionSnapshotClone(t *testing.T) {
	value := ExecutionSnapshot{
		Parameters: map[string]any{"nested": []any{map[string]any{"key": "value"}}},
		Output:     &Output{Content: []byte("one")},
		Failure: &Failure{
			RuntimeFailure: RuntimeFailure{
				Message: "failure",
				Diagnostics: []diagnostic.Diagnostic{{
					Message: "diagnostic",
					RelatedInformation: []diagnostic.RelatedInformation{{
						Message: "related",
					}},
				}},
			},
		},
	}
	cloned := value.Clone()
	cloned.Parameters["nested"].([]any)[0].(map[string]any)["key"] = "changed"
	cloned.Output.Content[0] = 't'
	cloned.Failure.Message = "changed"
	cloned.Failure.Diagnostics[0].Message = "changed"
	cloned.Failure.Diagnostics[0].RelatedInformation[0].Message = "changed"

	if value.Parameters["nested"].([]any)[0].(map[string]any)["key"] != "value" ||
		string(value.Output.Content) != "one" || value.Failure.Message != "failure" ||
		value.Failure.Diagnostics[0].Message != "diagnostic" ||
		value.Failure.Diagnostics[0].RelatedInformation[0].Message != "related" {
		t.Fatalf("clone mutated original snapshot: %+v", value)
	}
	if reflect.DeepEqual(value, cloned) {
		t.Fatal("clone did not retain independent mutable data")
	}
}
