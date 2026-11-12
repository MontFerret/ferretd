package grpc

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/MontFerret/api"
	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestExecutionContentProjectionPreservesPresence(t *testing.T) {
	for _, test := range []struct {
		name    string
		content *api.Content
	}{
		{name: "absent"},
		{name: "present empty", content: &api.Content{}},
		{name: "present empty bytes", content: &api.Content{Data: []byte{}}},
		{name: "partial", content: &api.Content{Metadata: api.Metadata{ContentType: "text/plain", Length: 100, LengthKnown: true}, Data: []byte("prefix")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			converted, err := toProtoExecution(exec.ExecutionSnapshot{State: exec.StateCompleted, Output: test.content})
			if err != nil {
				t.Fatal(err)
			}

			data, err := proto.Marshal(converted)
			if err != nil {
				t.Fatal(err)
			}

			var decoded executionv1.Execution
			if err := proto.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}

			if (decoded.Output == nil) != (test.content == nil) {
				t.Fatalf("output presence changed: %+v", decoded.Output)
			}

			if test.content != nil {
				if decoded.Output.ContentType != test.content.Metadata.ContentType || !bytes.Equal(decoded.Output.Data, test.content.Data) {
					t.Fatalf("output = %+v, content = %+v", decoded.Output, test.content)
				}

				if len(converted.Output.Data) > 0 {
					converted.Output.Data[0] = 'X'
					if bytes.Equal(converted.Output.Data, test.content.Data) {
						t.Fatal("conversion exposed retained bytes")
					}
				}
			}
		})
	}
}
