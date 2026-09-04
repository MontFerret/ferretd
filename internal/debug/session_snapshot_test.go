package debug

import (
	"reflect"
	"testing"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestSessionSnapshotClone(t *testing.T) {
	value := SessionSnapshot{
		HitBreakpointIDs: []BreakpointID{1},
		Parameters:       map[string]any{"nested": []any{map[string]any{"key": "value"}}},
		Options: exec.RuntimeOptions{
			WorkingDirectory:    "/runtime root",
			WorkingDirectorySet: true,
		},
		Output: &exec.RuntimeOutput{Content: []byte("one")},
		Failure: &exec.RuntimeFailure{
			Message: "failure",
			Diagnostics: []diagnostic.Diagnostic{{
				Message: "diagnostic",
				RelatedInformation: []diagnostic.RelatedInformation{{
					Message: "related",
				}},
			}},
		},
	}
	cloned := value.Clone()
	cloned.HitBreakpointIDs[0] = 2
	cloned.Parameters["nested"].([]any)[0].(map[string]any)["key"] = "changed"
	cloned.Options.WorkingDirectory = "/changed root"
	cloned.Output.Content[0] = 't'
	cloned.Failure.Message = "changed"
	cloned.Failure.Diagnostics[0].Message = "changed"
	cloned.Failure.Diagnostics[0].RelatedInformation[0].Message = "changed"

	if value.HitBreakpointIDs[0] != 1 ||
		value.Parameters["nested"].([]any)[0].(map[string]any)["key"] != "value" ||
		value.Options.WorkingDirectory != "/runtime root" ||
		string(value.Output.Content) != "one" || value.Failure.Message != "failure" ||
		value.Failure.Diagnostics[0].Message != "diagnostic" ||
		value.Failure.Diagnostics[0].RelatedInformation[0].Message != "related" {
		t.Fatalf("clone mutated original snapshot: %+v", value)
	}
	if reflect.DeepEqual(value, cloned) {
		t.Fatal("clone did not retain independent mutable data")
	}
}
