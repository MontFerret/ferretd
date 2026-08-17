package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	ferret "github.com/MontFerret/ferret/v2"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	localsource "github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type executionFixture struct {
	manager    *Manager
	workspaces *workspace.Manager
	workspace  *workspace.Workspace
	session    SessionSnapshot
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
	manager := New(workspaces)
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

func newHookedManager(
	t *testing.T,
	query string,
	options ...ferret.Option,
) (*Manager, SessionSnapshot, *ferret.Engine) {
	t.Helper()

	engine, err := ferret.New(options...)
	if err != nil {
		t.Fatalf("ferret.New: %v", err)
	}
	plan, err := engine.Compile(context.Background(), ferretsource.New("query.fql", query))
	if err != nil {
		_ = engine.Close()
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
		workspace.Compilation{Plan: plan, Source: snapshot},
		query,
	)
	manager := New(workspace.New())
	manager.sessions[session.id] = session
	manager.groups[workspaceID] = &workspaceGroup{
		sessions: map[SessionID]*Session{session.id: session},
	}
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = engine.Close()
	})

	return manager, session.Snapshot(), engine
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
