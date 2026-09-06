package debug

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestDebugFramePositionsAddressLocalsAndEvaluation(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	ctx := context.Background()

	created, err := fixture.manager.CreateSession(ctx, fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	debugger := fixture.runtime.latestDebugger()
	if debugger == nil {
		t.Fatal("debugger session was not created")
	}

	program := filepath.Join(fixture.workspace.Root(), "query.fql")
	wantFrames := []apidebugger.Frame{
		{Name: "callee", FunctionID: 91, Location: apisource.Location{SourceName: program, Position: apisource.Position{Line: 3, Column: 1}}},
		{Name: "recursive", FunctionID: 92, Location: apisource.Location{SourceName: program, Position: apisource.Position{Line: 7, Column: 1}}},
		{Name: "recursive", FunctionID: 92, Location: apisource.Location{SourceName: program, Position: apisource.Position{Line: 7, Column: 1}}},
		{Name: "main", FunctionID: 93, Location: apisource.Location{SourceName: program, Position: apisource.Position{Line: 11, Column: 1}}},
	}
	wantDisplays := []string{"10", "20", "30", "40"}
	debugger.frames = wantFrames
	for index, display := range wantDisplays {
		debugger.locals[index] = []apidebugger.Variable{
			{Name: "marker", Value: apidebugger.Value{Type: "Number", Display: display}},
			{Name: "@input", Param: true, Value: apidebugger.Value{Type: "Number", Display: "5"}},
		}
		debugger.values[index] = map[string]apidebugger.Value{
			"marker": {Type: "Number", Display: display},
		}
	}

	subscription, err := fixture.manager.WatchSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}

	t.Cleanup(subscription.Cancel)

	if _, err := fixture.manager.StartSession(ctx, created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	waitForState(t, subscription, StateStopped)

	frames, err := fixture.manager.Frames(ctx, created.ID)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}

	if !reflect.DeepEqual(frames, wantFrames) {
		t.Fatalf("Frames = %+v, want %+v", frames, wantFrames)
	}

	wantCommands := []debuggerCommand{{name: "start"}, {name: "frames"}}
	for index := range frames {
		scopes, err := fixture.manager.Scopes(ctx, created.ID, index)
		if err != nil {
			t.Fatalf("Scopes(%d): %v", index, err)
		}

		wantScopes := []Scope{
			{Kind: ScopeLocals, Name: "Locals", Variables: []apidebugger.Variable{
				{Name: "marker", Value: apidebugger.Value{Type: "Number", Display: wantDisplays[index]}},
			}},
			{Kind: ScopeParameters, Name: "Parameters", Variables: []apidebugger.Variable{
				{Name: "@input", Param: true, Value: apidebugger.Value{Type: "Number", Display: "5"}},
			}},
		}
		if !reflect.DeepEqual(scopes, wantScopes) {
			t.Fatalf("Scopes(%d) = %+v, want %+v", index, scopes, wantScopes)
		}

		value, err := fixture.manager.Evaluate(ctx, created.ID, index, "marker")
		if err != nil {
			t.Fatalf("Evaluate(%d): %v", index, err)
		}

		wantValue := apidebugger.Value{Type: "Number", Display: wantDisplays[index]}
		if value != wantValue {
			t.Fatalf("Evaluate(%d) = %+v, want %+v", index, value, wantValue)
		}

		wantCommands = append(wantCommands,
			debuggerCommand{name: "frame locals", frame: index},
			debuggerCommand{name: "evaluate frame", frame: index, expression: "marker"},
		)
	}

	if commands := debugger.recordedCommands(); !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("debugger commands = %+v, want %+v", commands, wantCommands)
	}
}
