package debug

import (
	"context"
	"reflect"
	"testing"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestSessionSnapshotClone(t *testing.T) {
	value := SessionSnapshot{
		HitBreakpointIDs: []apidebugger.BreakpointID{1},
		Parameters:       map[string]any{"nested": []any{map[string]any{"key": "value"}}},
		Options: exec.RuntimeOptions{
			WorkingDirectory: "/runtime root",
		},
		Output: &api.Content{Data: []byte("one")},
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
	cloned.Output.Data[0] = 't'
	cloned.Failure.Message = "changed"
	cloned.Failure.Diagnostics[0].Message = "changed"
	cloned.Failure.Diagnostics[0].RelatedInformation[0].Message = "changed"

	if value.HitBreakpointIDs[0] != 1 ||
		value.Parameters["nested"].([]any)[0].(map[string]any)["key"] != "value" ||
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

func TestDebugSessionRetainsIndependentOutputSnapshots(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	ctx := context.Background()

	created, err := fixture.manager.CreateSession(ctx, fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("result")
	debugger := fixture.runtime.latestDebugger()
	debugger.continueFn = func(context.Context) (*apidebugger.Event, error) {
		return &apidebugger.Event{
			Reason: apidebugger.ReasonCompleted,
			Output: &api.Content{Metadata: api.Metadata{ContentType: "text/plain"}, Data: content},
		}, nil
	}

	subscription, err := fixture.manager.WatchSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)

	if _, err := fixture.manager.StartSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	waitForState(t, subscription, StateStopped)

	if _, err := fixture.manager.ContinueSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	terminal := waitForState(t, subscription, StateCompleted)
	content[0] = 'X'

	if terminal.Output == nil || terminal.Output.Metadata.ContentType != "text/plain" || string(terminal.Output.Data) != "result" {
		t.Fatalf("terminal output = %+v", terminal.Output)
	}

	terminal.Output.Data[0] = 'Y'

	retained, err := fixture.manager.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if retained.Output == nil || string(retained.Output.Data) != "result" {
		t.Fatalf("retained output = %+v", retained.Output)
	}
}

func TestSessionSnapshotPreservesContentMetadata(t *testing.T) {
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
			original := SessionSnapshot{Output: test.content}

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
