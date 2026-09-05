package debug

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type debugFixture struct {
	manager    *Manager
	executions *exec.Manager
	runtime    *runtimeSpy
	workspaces *workspace.Manager
	workspace  *workspace.Workspace
	session    exec.SessionSnapshot
}

func mustNewExecutionManager(
	t testing.TB,
	workspaces *workspace.Manager,
	runtime api.Runtime,
) *exec.Manager {
	t.Helper()

	manager, err := exec.New(workspaces, runtime)
	if err != nil {
		_ = runtime.Close()
		t.Fatalf("exec.New: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = runtime.Close()
	})

	return manager
}

func mustNewManager(t testing.TB, executions *exec.Manager) *Manager {
	t.Helper()

	manager, err := New(executions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return manager
}

func assertPanics(t testing.TB, call func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()

	call()
}

func debuggerEvent(
	reason apidebugger.Reason,
	sourceName string,
	line int,
) *apidebugger.Event {
	return &apidebugger.Event{
		Reason: reason,
		Location: apisource.Range{
			Location: apisource.Location{
				SourceName: sourceName,
				Position:   apisource.Position{Line: line, Column: 1},
			},
		},
	}
}

func debuggerCommandsInclude(commands []debuggerCommand, expected ...string) bool {
	seen := make(map[string]bool, len(commands))
	for _, command := range commands {
		seen[command.name] = true
	}

	for _, name := range expected {
		if !seen[name] {
			return false
		}
	}

	return true
}

func blockDebuggerOnContinue(t testing.TB, debugger *debuggerSessionSpy) {
	t.Helper()

	if debugger == nil {
		t.Fatal("debugger session was not created")
	}

	debugger.continueFn = func(ctx context.Context) (*apidebugger.Event, error) {
		<-ctx.Done()

		return nil, ctx.Err()
	}
}

func newDebugFixture(t *testing.T, query string) debugFixture {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte(query), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	workspaces := workspace.New()

	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("workspace Open: %v", err)
	}

	runtime := newRuntimeSpy()
	executions := mustNewExecutionManager(t, workspaces, runtime)
	manager := mustNewManager(t, executions)

	session, err := executions.CreateSession(context.Background(), opened.ID(), "query.fql")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = executions.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	return debugFixture{
		manager:    manager,
		executions: executions,
		runtime:    runtime,
		workspaces: workspaces,
		workspace:  opened,
		session:    session,
	}
}
