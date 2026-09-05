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
		Output: &api.Output{Content: []byte("one")},
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
			Output: &api.Output{ContentType: "text/plain", Content: content},
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

	if terminal.Output == nil || terminal.Output.ContentType != "text/plain" || string(terminal.Output.Content) != "result" {
		t.Fatalf("terminal output = %+v", terminal.Output)
	}

	terminal.Output.Content[0] = 'Y'

	retained, err := fixture.manager.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if retained.Output == nil || string(retained.Output.Content) != "result" {
		t.Fatalf("retained output = %+v", retained.Output)
	}
}
