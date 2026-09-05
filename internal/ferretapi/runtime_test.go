package ferretapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apidiagnostics "github.com/MontFerret/api/diagnostics"
	"github.com/MontFerret/ferret/v2"
	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
)

func TestNewRequiresNativeEngine(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New did not panic for a nil native engine")
		}
	}()

	New(nil)
}

func TestRuntimeTranslatesSourceOptionsAndReusesPlan(t *testing.T) {
	runtime := newTestRuntime(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value.txt"), []byte("rooted"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sourcePath := filepath.Join(root, "query.fql")
	compiled, err := runtime.Compile(
		context.Background(),
		api.NewSource(sourcePath, "RETURN [@value, TO_STRING(IO::FS::READ(\"value.txt\"))]"),
		api.WithOptimizationLevel(api.OptimizationFull),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })

	if got := compiled.Params(); len(got) != 1 || got[0] != "value" {
		t.Fatalf("Params = %v, want [value]", got)
	}
	parameters := compiled.Params()
	parameters[0] = "changed"
	if got := compiled.Params(); len(got) != 1 || got[0] != "value" {
		t.Fatalf("Params after caller mutation = %v, want [value]", got)
	}

	for _, value := range []string{"first", "second"} {
		session, err := compiled.NewSession(
			context.Background(),
			api.WithParam("value", value),
			api.WithOutputContentType("application/json"),
			api.WithFSRoot(root),
		)
		if err != nil {
			t.Fatalf("NewSession(%s): %v", value, err)
		}

		output, runErr := session.Run(context.Background())
		closeErr := session.Close()
		if err := errors.Join(runErr, closeErr); err != nil {
			t.Fatalf("session(%s): %v", value, err)
		}
		want := "[\"" + value + "\",\"rooted\"]"
		if output.ContentType != "application/json" || string(output.Content) != want {
			t.Fatalf("output(%s) = %+v, want %s", value, output, want)
		}
	}
}

func TestRuntimeRunUsesUniversalSessionOptions(t *testing.T) {
	runtime := newTestRuntime(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "value.txt"), []byte("rooted"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	output, err := runtime.Run(
		context.Background(),
		api.NewSource(
			filepath.Join(root, "query.fql"),
			"RETURN [@value, TO_STRING(IO::FS::READ(\"value.txt\"))]",
		),
		api.WithParam("value", "direct"),
		api.WithOutputContentType("application/json"),
		api.WithFSRoot(root),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output.ContentType != "application/json" || string(output.Content) != `["direct","rooted"]` {
		t.Fatalf("output = %+v", output)
	}
}

func TestRuntimeCopiesOutputAndTranslatesDebugSession(t *testing.T) {
	runtime := newTestRuntime(t)
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "query.fql")

	compiled, err := runtime.Compile(ctx, api.NewSource(sourcePath, "RETURN 1"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })

	first, err := compiled.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession first: %v", err)
	}
	firstOutput, err := first.Run(ctx)
	if err != nil {
		t.Fatalf("Run first: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	firstOutput.Content[0] = '9'

	second, err := compiled.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession second: %v", err)
	}
	secondOutput, err := second.Run(ctx)
	if err != nil {
		t.Fatalf("Run second: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}
	if string(secondOutput.Content) != "1" {
		t.Fatalf("second output = %q, want independent 1", secondOutput.Content)
	}

	debugPlan, err := runtime.CompileDebug(
		ctx,
		api.NewSource(sourcePath, "RETURN 1"),
		api.WithOptimizationLevel(api.OptimizationNone),
	)
	if err != nil {
		t.Fatalf("CompileDebug: %v", err)
	}
	t.Cleanup(func() { _ = debugPlan.Close() })
	debugSession, err := debugPlan.NewDebugSession(ctx)
	if err != nil {
		t.Fatalf("NewDebugSession: %v", err)
	}
	t.Cleanup(func() { _ = debugSession.Close() })

	event, err := debugSession.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if event.Reason != apidebugger.ReasonEntry || event.Location.SourceName != sourcePath {
		t.Fatalf("entry event = %+v", event)
	}
	completed, err := debugSession.Continue(ctx)
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if completed.Reason != apidebugger.ReasonCompleted || completed.Output == nil ||
		string(completed.Output.Content) != "1" {
		t.Fatalf("completed event = %+v", completed)
	}
}

func TestDebuggerEventTranslation(t *testing.T) {
	wantErr := errors.New("runtime stopped")
	nativeOutput := &ferret.Output{
		ContentType: "application/json",
		Content:     []byte(`{"result":1}`),
	}
	session := &debugSession{source: api.NewSource("/workspace/query.fql", "RETURN 1")}
	event := session.convertEvent(&ferret.DebugEvent{
		Error:            wantErr,
		Output:           nativeOutput,
		Reason:           ferret.DebugReasonBreakpoint,
		HitBreakpointIDs: []ferret.DebugBreakpointID{7, 8},
		Location: ferret.Range{
			Location: ferret.Location{
				File:     "/workspace/query.fql",
				Position: ferret.Position{Line: 3, Column: 4},
			},
			Span: ferret.Span{Start: 10, End: 20},
		},
		Depth: 2,
	})

	if event == nil || !errors.Is(event.Error, wantErr) ||
		event.Reason != apidebugger.ReasonBreakpoint ||
		event.Location.SourceName != "/workspace/query.fql" ||
		event.Location.Position != (api.Position{Line: 3, Column: 4}) ||
		event.Location.Span != (api.Span{Start: 10, End: 20}) || event.Depth != 2 ||
		len(event.HitBreakpointIDs) != 2 || event.HitBreakpointIDs[0] != 7 ||
		event.HitBreakpointIDs[1] != 8 || event.Output == nil ||
		event.Output.ContentType != "application/json" || string(event.Output.Content) != `{"result":1}` {
		t.Fatalf("event = %+v", event)
	}

	nativeOutput.Content[0] = 'x'
	if string(event.Output.Content) != `{"result":1}` {
		t.Fatalf("event output changed with native output: %q", event.Output.Content)
	}
}

func TestRuntimeConvertsDiagnosticsAndPreservesNativeCause(t *testing.T) {
	runtime := newTestRuntime(t)
	source := api.NewSource("/absolute/query.fql", "RETURN")

	_, err := runtime.Compile(context.Background(), source)
	if err == nil {
		t.Fatal("Compile unexpectedly succeeded")
	}

	var nativeSet *ferretdiagnostics.DiagnosticSet
	var native *ferretdiagnostics.Diagnostic
	if !errors.As(err, &nativeSet) && !errors.As(err, &native) {
		t.Fatalf("error does not preserve native diagnostic cause: %v", err)
	}
	var portable apidiagnostics.Diagnostics
	if !errors.As(err, &portable) || len(portable) == 0 {
		t.Fatalf("portable diagnostics = %+v, want non-empty", portable)
	}
	if portable[0].Source != source || portable[0].Message == "" ||
		len(portable[0].Annotations) == 0 ||
		portable[0].Annotations[0].Range.SourceName != source.Name {
		t.Fatalf("portable diagnostic = %+v", portable[0])
	}
}

func TestRuntimeRejectsUnsupportedPlanOptions(t *testing.T) {
	runtime := newTestRuntime(t)
	source := api.NewAnonymousSource("RETURN 1")
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "ordinary non-default",
			call: func() error {
				_, err := runtime.Compile(
					context.Background(),
					source,
					api.WithOptimizationLevel(api.OptimizationNone),
				)

				return err
			},
		},
		{
			name: "debug non-default",
			call: func() error {
				_, err := runtime.CompileDebug(
					context.Background(),
					source,
					api.WithOptimizationLevel(api.OptimizationFull),
				)

				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil || !strings.Contains(err.Error(), "not supported") {
				t.Fatalf("error = %v, want unsupported option", err)
			}
		})
	}
}

func TestDebuggerValueAndBreakpointTranslation(t *testing.T) {
	breakpoint := convertBreakpoint(ferret.DebugBreakpoint{
		ID:         7,
		PointID:    8,
		FunctionID: 9,
		RequestedLocation: ferret.Location{
			File:     "/workspace/query.fql",
			Position: ferret.Position{Line: 2, Column: 3},
		},
		Location: ferret.Range{
			Location: ferret.Location{
				File:     "/workspace/query.fql",
				Position: ferret.Position{Line: 4, Column: 5},
			},
			Span: ferret.Span{Start: 10, End: 20},
		},
		BindingMode: ferret.DebugBreakpointBindExact,
		Bound:       true,
	})
	if breakpoint.ID != 7 || breakpoint.RequestedLocation.SourceName != "/workspace/query.fql" ||
		breakpoint.RequestedLocation.Line != 2 || breakpoint.Location.Line != 4 ||
		breakpoint.Location.Span.Start != 10 || breakpoint.PointID != 8 ||
		breakpoint.FunctionID != 9 || breakpoint.BindingMode != apidebugger.BreakpointBindExact ||
		!breakpoint.Bound {
		t.Fatalf("breakpoint = %+v", breakpoint)
	}

	variables := convertVariables([]ferret.DebugVariable{{
		Name: "value",
		Value: ferret.DebugValue{
			Type:      "array",
			Display:   "[1]",
			Reference: 9,
		},
		Mutable: true,
		Param:   true,
	}})
	if len(variables) != 1 || variables[0].Name != "value" ||
		variables[0].Value.Reference != 9 || !variables[0].Mutable || !variables[0].Param {
		t.Fatalf("variables = %+v", variables)
	}

	if nativeBreakpointMode(apidebugger.BreakpointBindNextExecutableInSource) !=
		ferret.DebugBreakpointBindNextExecutableInFile {
		t.Fatal("default breakpoint mode was not translated to native in-file binding")
	}
	if convertBreakpointMode(ferret.DebugBreakpointBindNextExecutableInFunction) !=
		apidebugger.BreakpointBindNextExecutableInFunction {
		t.Fatal("native in-function breakpoint mode was not translated")
	}
}

func TestRuntimeResourcesCloseExactlyOnce(t *testing.T) {
	var engineCloses atomic.Int64
	var planCloses atomic.Int64
	var sessionCloses atomic.Int64
	engine, err := ferret.New(
		ferret.WithEngineCloseHook(func() error {
			engineCloses.Add(1)

			return nil
		}),
		ferret.WithPlanCloseHook(func() error {
			planCloses.Add(1)

			return nil
		}),
		ferret.WithSessionCloseHook(func() error {
			sessionCloses.Add(1)

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("ferret.New: %v", err)
	}
	runtime := New(engine)

	compiled, err := runtime.Compile(context.Background(), api.NewAnonymousSource("RETURN 1"))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	session, err := compiled.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	debugPlan, err := runtime.CompileDebug(context.Background(), api.NewAnonymousSource("RETURN 1"))
	if err != nil {
		t.Fatalf("CompileDebug: %v", err)
	}
	debugSession, err := debugPlan.NewDebugSession(context.Background())
	if err != nil {
		t.Fatalf("NewDebugSession: %v", err)
	}

	for range 2 {
		if err := session.Close(); err != nil {
			t.Fatalf("Session.Close: %v", err)
		}
		if err := debugSession.Close(); err != nil {
			t.Fatalf("DebugSession.Close: %v", err)
		}
		if err := compiled.Close(); err != nil {
			t.Fatalf("Plan.Close: %v", err)
		}
		if err := debugPlan.Close(); err != nil {
			t.Fatalf("DebugPlan.Close: %v", err)
		}
		if err := runtime.Close(); err != nil {
			t.Fatalf("Runtime.Close: %v", err)
		}
	}

	if sessionCloses.Load() != 2 || planCloses.Load() != 2 || engineCloses.Load() != 1 {
		t.Fatalf(
			"close calls = session %d, plan %d, engine %d; want 2, 2, 1",
			sessionCloses.Load(),
			planCloses.Load(),
			engineCloses.Load(),
		)
	}
}

func newTestRuntime(t testing.TB) *Runtime {
	t.Helper()

	engine, err := ferret.New()
	if err != nil {
		t.Fatalf("ferret.New: %v", err)
	}
	runtime := New(engine)
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return runtime
}
