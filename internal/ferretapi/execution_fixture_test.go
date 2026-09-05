package ferretapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/ferretapi"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type executionFixture struct {
	manager    *exec.Manager
	workspaces *workspace.Manager
	workspace  *workspace.Workspace
	session    exec.SessionSnapshot
}

func mustNewManager(t testing.TB, workspaces *workspace.Manager) *exec.Manager {
	t.Helper()

	engine, err := ferret.New()
	if err != nil {
		t.Fatalf("ferret.New: %v", err)
	}

	runtime := ferretapi.New(engine)

	manager, err := exec.New(workspaces, runtime)
	if err != nil {
		_ = runtime.Close()
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = runtime.Close()
	})

	return manager
}

func newExecutionFixture(t *testing.T, query string) executionFixture {
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

	manager := mustNewManager(t, workspaces)

	session, err := manager.CreateSession(context.Background(), opened.ID(), "query.fql")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	return executionFixture{
		manager:    manager,
		workspaces: workspaces,
		workspace:  opened,
		session:    session,
	}
}

func runAndObserve(t *testing.T, manager *exec.Manager, id exec.ExecutionID) (exec.ExecutionSnapshot, []exec.Event) {
	t.Helper()

	subscription, err := manager.WatchExecution(context.Background(), id)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	defer subscription.Cancel()

	if subscription.Current.Kind != exec.EventCreated || subscription.Current.Sequence != 1 {
		t.Fatalf("current event = %+v", subscription.Current)
	}

	running, err := manager.RunExecution(context.Background(), id)
	if err != nil {
		t.Fatalf("RunExecution: %v", err)
	}

	if running.State != exec.StateRunning {
		t.Fatalf("RunExecution state = %v, want running", running.State)
	}

	events := []exec.Event{subscription.Current}
	for event := range subscription.Events {
		events = append(events, event)
	}

	for watchErr := range subscription.Errors {
		if watchErr != nil {
			t.Fatalf("watch error: %v", watchErr)
		}
	}

	terminal := events[len(events)-1].Snapshot
	if !terminal.State.Terminal() {
		t.Fatalf("last event = %+v, want terminal", events[len(events)-1])
	}

	return terminal, events
}

func writeSourceFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func runSessionOutput(t *testing.T, manager *exec.Manager, sessionID exec.SessionID) string {
	t.Helper()

	created, err := manager.CreateExecution(context.Background(), sessionID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	terminal, _ := runAndObserve(t, manager, created.ID)
	if terminal.State != exec.StateCompleted || terminal.Output == nil {
		t.Fatalf("terminal execution = %+v, want completed output", terminal)
	}

	return string(terminal.Output.Content)
}

func assertFailure(t *testing.T, terminal exec.ExecutionSnapshot, category exec.FailureCategory, wantOutput bool) {
	t.Helper()

	if terminal.State != exec.StateFailed || terminal.Failure == nil || terminal.Failure.Category != category ||
		terminal.Failure.Message == "" {
		t.Fatalf("terminal = %+v, want failed category %v", terminal, category)
	}

	if (terminal.Output != nil) != wantOutput {
		t.Fatalf("output = %+v, want present %t", terminal.Output, wantOutput)
	}
}
