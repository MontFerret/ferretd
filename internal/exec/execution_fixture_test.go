package exec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/api"
	localsource "github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type executionFixture struct {
	manager    *Manager
	workspaces *workspace.Manager
	workspace  *workspace.Workspace
	session    SessionSnapshot
	runtime    *runtimeSpy
}

func mustNewManager(t testing.TB, workspaces *workspace.Manager) *Manager {
	t.Helper()

	return newManagerWithRuntime(t, workspaces, newRuntimeSpy())
}

func newManagerWithRuntime(t testing.TB, workspaces *workspace.Manager, runtime *runtimeSpy) *Manager {
	t.Helper()

	manager, err := New(workspaces, runtime)
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

func assertPanics(t testing.TB, call func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()

	call()
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
	runtime := newRuntimeSpy()
	manager := newManagerWithRuntime(t, workspaces, runtime)
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
		runtime:    runtime,
	}
}

func newHookedManager(
	t *testing.T,
	query string,
	options ...runtimeSpyOption,
) (*Manager, SessionSnapshot, *runtimeSpy) {
	t.Helper()

	runtime := newRuntimeSpy(options...)
	plan, err := runtime.Compile(context.Background(), api.NewSource("/query.fql", query))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	workspaceID := workspace.ID("workspace")
	snapshot := workspace.SourceSnapshot{
		Workspace:    workspaceID,
		RelativePath: "query.fql",
		URI:          localsource.URI("file:///query.fql"),
		Revision:     1,
	}
	session := newSession(
		SessionID("session"),
		snapshot,
		plan,
		query,
		"/",
		func(ctx context.Context) (api.Plan, error) {
			return runtime.CompileDebug(ctx, api.NewSource("/query.fql", query))
		},
	)
	manager, err := New(workspace.New(), runtime)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	creation, err := manager.sessions.beginCreate(workspaceID)
	if err != nil {
		t.Fatalf("begin Session creation: %v", err)
	}
	if err := manager.sessions.commitCreate(context.Background(), creation, session); err != nil {
		t.Fatalf("commit Session creation: %v", err)
	}
	manager.sessions.finishCreate(creation)
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = runtime.Close()
	})

	return manager, session.snapshot(), runtime
}

func retainedSession(t testing.TB, manager *Manager, id SessionID) *sessionEntry {
	t.Helper()

	manager.sessions.mu.RLock()
	entry := manager.sessions.entries[id]
	manager.sessions.mu.RUnlock()
	if entry == nil {
		t.Fatalf("Session %q is not retained", id)
	}

	return entry
}

func retainedExecution(t testing.TB, manager *Manager, id ExecutionID) *executionEntry {
	t.Helper()

	manager.executions.mu.RLock()
	entry := manager.executions.entries[id]
	manager.executions.mu.RUnlock()
	if entry == nil {
		t.Fatalf("Execution %q is not retained", id)
	}

	return entry
}

func runAndObserve(t *testing.T, manager *Manager, id ExecutionID) (ExecutionSnapshot, []Event) {
	t.Helper()

	subscription, err := manager.WatchExecution(context.Background(), id)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	defer subscription.Cancel()
	if subscription.Current.Kind != EventCreated || subscription.Current.Sequence != 1 {
		t.Fatalf("current event = %+v", subscription.Current)
	}

	running, err := manager.RunExecution(context.Background(), id)
	if err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if running.State != StateRunning {
		t.Fatalf("RunExecution state = %v, want running", running.State)
	}

	events := []Event{subscription.Current}
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

func parameterOutput(_ context.Context, options sessionOptionsSpy) (api.Output, error) {
	content, err := json.Marshal(options.params["value"])

	return api.Output{ContentType: options.contentType, Content: content}, err
}

func canceledOutput(ctx context.Context, _ sessionOptionsSpy) (api.Output, error) {
	<-ctx.Done()

	return api.Output{}, ctx.Err()
}
