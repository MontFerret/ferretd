package exec

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestSessionFilesystemPrecedence(t *testing.T) {
	root, override := t.TempDir(), t.TempDir()

	canonical, err := filepath.EvalSymlinks(override)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name, root, override, want string
		calls                      int
	}{
		{name: "no override"},
		{name: "workspace", root: root, want: root, calls: 1},
		{name: "explicit", root: root, override: override, want: canonical, calls: 1},
		{name: "explicit without workspace", override: override, want: canonical, calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, err := newRuntimeInput(nil, RuntimeOptions{WorkingDirectory: test.override})
			if err != nil {
				t.Fatal(err)
			}

			runtime := newExecutionRuntime(runtimeTarget{fsRoot: test.root}, input)
			t.Cleanup(func() { runtime.cancel(errExecutionCanceled) })

			options, err := newSessionOptionsSpy(runtime.sessionOptions())
			if err != nil {
				t.Fatal(err)
			}

			if options.fsRoot != test.want || options.fsRootCalls != test.calls {
				t.Fatalf("root = %q in %d options, want %q in %d", options.fsRoot, options.fsRootCalls, test.want, test.calls)
			}

			if test.override == "" && runtime.options().WorkingDirectory != "" {
				t.Fatal("workspace fallback leaked into the retained override")
			}
		})
	}
}

func TestExecutionRetainsOutputBeforeSessionClose(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "partial output", err: errors.New("run failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte("result")
			manager, snapshot, runtime := newHookedManager(t, "RETURN 1", withSessionCloseHook(func() error {
				content[0] = 'X'

				return nil
			}))
			runtime.run = func(context.Context, sessionOptionsSpy) (api.Output, error) {
				return api.Output{ContentType: "text/plain", Content: content}, test.err
			}

			created, err := manager.CreateExecution(context.Background(), snapshot.ID, nil, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			terminal, _ := runAndObserve(t, manager, created.ID)
			if terminal.Output == nil || terminal.Output.ContentType != "text/plain" || string(terminal.Output.Content) != "result" {
				t.Fatalf("retained output = %+v", terminal.Output)
			}

			if test.err != nil {
				assertFailure(t, terminal, FailureRuntime, true)
			}

			terminal.Output.Content[0] = 'Y'

			retained, err := manager.GetExecution(context.Background(), created.ID)
			if err != nil {
				t.Fatal(err)
			}

			if string(retained.Output.Content) != "result" {
				t.Fatal("snapshot mutation changed retained output")
			}
		})
	}
}

func TestSessionOwnsDeclaredParameterNames(t *testing.T) {
	runtime := newRuntimeSpy()
	runtime.parameters = []string{"value"}
	plan := &planSpy{runtime: runtime}
	created := newSession("session", workspace.SourceSnapshot{}, plan, "", "", nil)
	runtime.parameters[0] = "changed"

	if names := created.snapshot().Parameters; len(names) != 1 || names[0] != "value" {
		t.Fatalf("retained parameters = %v", names)
	}
}

func TestRuntimeParametersAreIsolatedFromOptionConsumers(t *testing.T) {
	parameters := Parameters{"nested": map[string]any{"items": []any{"original"}}}

	input, err := newRuntimeInput(parameters, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	runtime := newExecutionRuntime(runtimeTarget{}, input)
	t.Cleanup(func() { runtime.cancel(errExecutionCanceled) })

	options, err := newSessionOptionsSpy(runtime.sessionOptions())
	if err != nil {
		t.Fatal(err)
	}

	options.params["nested"].(map[string]any)["items"].([]any)[0] = "runtime mutation"
	for name, value := range map[string]Parameters{"caller": parameters, "retained": runtime.parameters()} {
		if got := value["nested"].(map[string]any)["items"].([]any)[0]; got != "original" {
			t.Fatalf("%s parameter = %v, want original", name, got)
		}
	}
}

func TestWorkspaceCloseDoesNotCloseBorrowedRuntime(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	if err := fixture.workspaces.Close(context.Background(), fixture.workspace.ID()); err != nil {
		t.Fatal(err)
	}

	if fixture.runtime.closeCalls.Load() != 0 {
		t.Fatal("workspace close closed the borrowed runtime")
	}

	if _, err := fixture.manager.GetSession(context.Background(), fixture.session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("child session after workspace close: %v", err)
	}
}
