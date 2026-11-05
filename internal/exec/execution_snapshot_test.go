package exec

import (
	"reflect"
	"testing"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/diagnostic"
)

func TestExecutionSnapshotClone(t *testing.T) {
	value := ExecutionSnapshot{
		Parameters: map[string]any{"nested": []any{map[string]any{"key": "value"}}},
		Options: RuntimeOptions{
			WorkingDirectory: "/runtime root",
		},
		Output: &api.Content{Data: []byte("one")},
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
	cloned.Options.WorkingDirectory = "/changed root"
	cloned.Output.Data[0] = 't'
	cloned.Failure.Message = "changed"
	cloned.Failure.Diagnostics[0].Message = "changed"
	cloned.Failure.Diagnostics[0].RelatedInformation[0].Message = "changed"

	if value.Parameters["nested"].([]any)[0].(map[string]any)["key"] != "value" ||
		value.Options.WorkingDirectory != "/runtime root" ||
		string(value.Output.Data) != "one" || value.Failure.Message != "failure" ||
		value.Failure.Diagnostics[0].Message != "diagnostic" ||
		value.Failure.Diagnostics[0].RelatedInformation[0].Message != "related" {
		t.Fatalf("clone mutated original snapshot: %+v", value)
	}

	if reflect.DeepEqual(value, cloned) {
		t.Fatal("clone did not retain independent mutable data")
	}
}

func TestExecutionSnapshotPreservesContentMetadata(t *testing.T) {
	for _, test := range []struct {
		name    string
		content *api.Content
	}{
		{name: "absent"},
		{name: "present empty", content: &api.Content{}},
		{name: "present empty bytes", content: &api.Content{Data: []byte{}}},
		{name: "unknown length", content: &api.Content{Metadata: api.Metadata{ContentType: "text/plain", Length: 17}, Data: []byte("prefix")}},
		{name: "known complete length with prefix", content: &api.Content{Metadata: api.Metadata{ContentType: "text/plain", Length: 100, LengthKnown: true}, Data: []byte("prefix")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := ExecutionSnapshot{Output: test.content}

			first, second := original.Clone(), original.Clone()
			if !reflect.DeepEqual(first.Output, test.content) {
				t.Fatalf("cloned content = %+v, want %+v", first.Output, test.content)
			}

			if first.Output != nil {
				first.Output.Metadata.Length = 999

				first.Output.Metadata.LengthKnown = !first.Output.Metadata.LengthKnown
				if len(first.Output.Data) > 0 {
					first.Output.Data[0] = 'X'
				}
			}

			if !reflect.DeepEqual(second.Output, test.content) {
				t.Fatal("snapshot mutation changed another observer or retained metadata")
			}
		})
	}
}
