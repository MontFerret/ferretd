package exec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/runtimes"
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

	manager, err := New(workspaces, newTestOwner(t, runtime))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
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

	session, err := manager.CreateSession(context.Background(), opened.ID(), "query.fql", "")
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
	owner := newTestOwner(t, runtime)

	binding, err := owner.Acquire(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}

	session := newSession(
		SessionID("session"),
		snapshot,
		plan,
		runtime.parameters,
		query,
		"/",
		func(ctx context.Context) (api.Plan, error) {
			return runtime.CompileDebug(ctx, api.NewSource("/query.fql", query))
		},
		binding,
	)

	manager, err := New(workspace.New(), owner)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	creation, err := manager.sessions.beginCreate(workspaceID, newTestConstruction(t))
	if err != nil {
		t.Fatalf("begin Session creation: %v", err)
	}

	if err := manager.sessions.commitCreate(context.Background(), creation, session); err != nil {
		t.Fatalf("commit Session creation: %v", err)
	}

	_ = manager.sessions.finishCreate(creation)
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
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

func parameterOutput(_ context.Context, options sessionOptionsSpy) (*api.Content, error) {
	content, err := json.Marshal(options.params["value"])

	return &api.Content{Metadata: api.Metadata{ContentType: options.contentType}, Data: content}, err
}

func canceledOutput(ctx context.Context, _ sessionOptionsSpy) (*api.Content, error) {
	<-ctx.Done()

	return nil, ctx.Err()
}

func newTestOwner(t testing.TB, runtime api.Runtime) *runtimes.Owner {
	t.Helper()

	catalog, err := runtimes.NewCatalog(runtimes.Config{})
	if err != nil {
		t.Fatal(err)
	}

	owner, err := runtimes.NewOwner(catalog, runtime)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = owner.Close(context.Background()) })

	return owner
}

func newTestConstruction(t testing.TB) *construction {
	t.Helper()
	operation, _ := newConstruction(t.Context())
	t.Cleanup(operation.finish)

	return operation
}

func newTestBinding(t testing.TB, runtime api.Runtime) *runtimes.Binding {
	t.Helper()
	owner := newTestOwner(t, runtime)

	binding, err := owner.Acquire(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = binding.Release() })

	return binding
}
