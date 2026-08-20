package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestCreateSessionCompilesImmutableSourceAndParameters(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN [@second, @first]")
	got := fixture.session
	if got.ID == "" || got.Source.Workspace != fixture.workspace.ID() ||
		got.Source.RelativePath != "query.fql" || got.Source.URI == "" || got.Source.Revision != 1 {
		t.Fatalf("Session = %+v", got)
	}
	if _, err := uuid.Parse(string(got.ID)); err != nil {
		t.Fatalf("Session ID is not a UUID: %v", err)
	}
	if want := []string{"second", "first"}; !reflect.DeepEqual(got.Parameters, want) {
		t.Fatalf("parameters = %#v, want %#v", got.Parameters, want)
	}

	got.Parameters[0] = "mutated"
	stored, err := fixture.manager.GetSession(context.Background(), got.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Parameters[0] != "second" {
		t.Fatalf("stored parameters = %#v, want defensive copy", stored.Parameters)
	}

	second, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.workspace.ID(),
		"query.fql",
	)
	if err != nil {
		t.Fatalf("second CreateSession: %v", err)
	}
	if second.ID == got.ID || second.Source != got.Source {
		t.Fatalf("second Session = %+v, first = %+v", second, got)
	}
}

func TestCreateSessionRefreshesSavedSourceAndKeepsSessionsImmutable(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	first := fixture.session

	writeSourceFile(t, fixture.workspace.Root(), "query.fql", "RETURN 2")
	second, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.workspace.ID(),
		"query.fql",
	)
	if err != nil {
		t.Fatalf("second CreateSession: %v", err)
	}
	if first.Source.Revision != 1 || second.Source.Revision != 2 {
		t.Fatalf("source revisions = %d and %d, want 1 and 2",
			first.Source.Revision, second.Source.Revision)
	}

	if got := runSessionOutput(t, fixture.manager, first.ID); got != "1" {
		t.Fatalf("first Session output = %q, want 1", got)
	}
	if got := runSessionOutput(t, fixture.manager, second.ID); got != "2" {
		t.Fatalf("second Session output = %q, want 2", got)
	}

	third, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.workspace.ID(),
		"query.fql",
	)
	if err != nil {
		t.Fatalf("unchanged CreateSession: %v", err)
	}
	if third.Source.Revision != second.Source.Revision {
		t.Fatalf("unchanged source revision = %d, want %d",
			third.Source.Revision, second.Source.Revision)
	}
}

func TestCreateSessionRefreshesCompilationAndUnavailableDiagnostics(t *testing.T) {
	t.Run("compilation failure", func(t *testing.T) {
		fixture := newExecutionFixture(t, "RETURN 1")
		writeSourceFile(t, fixture.workspace.Root(), "query.fql", "RETURN missing")

		_, err := fixture.manager.CreateSession(
			context.Background(),
			fixture.workspace.ID(),
			"query.fql",
		)
		var compilation *CompilationError
		if !errors.As(err, &compilation) || !errors.Is(err, ErrCompilationFailed) {
			t.Fatalf("CreateSession error = %v, want CompilationError", err)
		}
		if compilation.Source.Revision != 2 || len(compilation.Diagnostics) == 0 {
			t.Fatalf("CompilationError = %+v, want revision 2 diagnostics", compilation)
		}

		writeSourceFile(t, fixture.workspace.Root(), "query.fql", "RETURN 3")
		recovered, err := fixture.manager.CreateSession(
			context.Background(),
			fixture.workspace.ID(),
			"query.fql",
		)
		if err != nil {
			t.Fatalf("recovered CreateSession: %v", err)
		}
		if recovered.Source.Revision != 3 || runSessionOutput(t, fixture.manager, recovered.ID) != "3" {
			t.Fatalf("recovered Session = %+v", recovered)
		}
	})

	t.Run("missing source", func(t *testing.T) {
		fixture := newExecutionFixture(t, "RETURN 1")
		if err := os.Remove(filepath.Join(fixture.workspace.Root(), "query.fql")); err != nil {
			t.Fatalf("Remove: %v", err)
		}

		_, err := fixture.manager.CreateSession(
			context.Background(),
			fixture.workspace.ID(),
			"query.fql",
		)
		var compilation *CompilationError
		if !errors.As(err, &compilation) || !errors.Is(err, workspace.ErrDocumentUnavailable) {
			t.Fatalf("CreateSession error = %v, want unavailable CompilationError", err)
		}
		if compilation.Source.Revision != 2 || len(compilation.Diagnostics) == 0 {
			t.Fatalf("CompilationError = %+v, want revision 2 diagnostics", compilation)
		}
	})
}

func TestOldSessionLazilyCompilesMatchingDebugPlanAfterRefresh(t *testing.T) {
	fixture := newExecutionFixture(t, "LET value = 1\nRETURN value")
	first := fixture.session
	writeSourceFile(t, fixture.workspace.Root(), "query.fql", "RETURN 2")
	second, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.workspace.ID(),
		"query.fql",
	)
	if err != nil {
		t.Fatalf("second CreateSession: %v", err)
	}
	if second.Source.Revision != 2 {
		t.Fatalf("second source revision = %d, want 2", second.Source.Revision)
	}

	target, err := fixture.manager.AcquireDebugTarget(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("AcquireDebugTarget: %v", err)
	}
	defer target.Release()
	if target.Source() != first.Source || target.SourceText() != "LET value = 1\nRETURN value" {
		t.Fatalf("debug target = source %+v text %q, want first Session snapshot",
			target.Source(), target.SourceText())
	}

	debugSession, err := target.NewDebugSession(context.Background())
	if err != nil {
		t.Fatalf("NewDebugSession: %v", err)
	}
	defer func() { _ = debugSession.Close() }()
	if _, err := debugSession.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	event, err := debugSession.Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if event.Reason != ferret.DebugReasonCompleted || event.Output == nil || string(event.Output.Content) != "1" {
		t.Fatalf("debug completion = %+v, want first Session output 1", event)
	}
}

func TestCreateSessionReturnsStructuredDiagnosticsWithoutRegistration(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN missing")
	workspaces := workspace.New()
	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	manager := New(workspaces)
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	_, err = manager.CreateSession(context.Background(), opened.ID(), "query.fql")
	if !errors.Is(err, ErrCompilationFailed) {
		t.Fatalf("CreateSession error = %v, want ErrCompilationFailed", err)
	}
	var compilation *CompilationError
	if !errors.As(err, &compilation) {
		t.Fatalf("CreateSession error type = %T, want CompilationError", err)
	}
	if compilation.Source.RelativePath != "query.fql" || compilation.Source.URI == "" ||
		len(compilation.Diagnostics) == 0 {
		t.Fatalf("CompilationError = %+v", compilation)
	}
	if compilation.Diagnostics[0].URI != compilation.Source.URI ||
		compilation.Diagnostics[0].Code == "" || compilation.Diagnostics[0].Message == "" {
		t.Fatalf("diagnostic = %+v", compilation.Diagnostics[0])
	}

	manager.mu.RLock()
	registered := len(manager.sessions)
	manager.mu.RUnlock()
	if registered != 0 {
		t.Fatalf("registered Sessions = %d, want 0", registered)
	}
}

func TestCloseSessionRunsPlanCloseOnceAndIsIdempotent(t *testing.T) {
	var closes int
	manager, session, _ := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closes++

		return nil
	}))

	if err := manager.CloseSession(context.Background(), session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if err := manager.CloseSession(context.Background(), session.ID); err != nil {
		t.Fatalf("second CloseSession: %v", err)
	}
	if closes != 1 {
		t.Fatalf("plan closes = %d, want 1", closes)
	}
	if _, err := manager.GetSession(context.Background(), session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionCloseCallerTimeoutDoesNotStopCleanup(t *testing.T) {
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	manager, session, _ := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		close(closeStarted)
		<-releaseClose

		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.CloseSession(ctx, session.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseSession error = %v, want context.Canceled", err)
	}
	<-closeStarted
	close(releaseClose)
	if err := manager.CloseSession(context.Background(), session.ID); err != nil {
		t.Fatalf("wait for committed close: %v", err)
	}
}

func TestCloseWorkspaceWaitsForInFlightSessionCreation(t *testing.T) {
	manager := New(workspace.New())
	workspaceID := workspace.ID("workspace")
	if err := manager.beginSessionCreate(workspaceID); err != nil {
		t.Fatalf("beginSessionCreate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.CloseWorkspace(ctx, workspaceID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseWorkspace error = %v, want context.Canceled", err)
	}
	manager.finishSessionCreate(workspaceID)
	if err := manager.CloseWorkspace(context.Background(), workspaceID); err != nil {
		t.Fatalf("wait for CloseWorkspace: %v", err)
	}
	manager.mu.RLock()
	_, retained := manager.groups[workspaceID]
	manager.mu.RUnlock()
	if retained {
		t.Fatal("workspace group retained after committed close")
	}
}

func TestParentCloseDoesNotReadoptSettledExecution(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		ExecutionOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	fixture.manager.mu.RLock()
	execution := fixture.manager.executions[created.ID]
	fixture.manager.mu.RUnlock()
	if err := fixture.manager.CloseExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("CloseExecution: %v", err)
	}
	fixture.manager.detachKnownExecution(execution)

	fixture.manager.mu.RLock()
	_, retained := fixture.manager.closingExecutions[created.ID]
	fixture.manager.mu.RUnlock()
	if retained {
		t.Fatal("settled Execution was reinserted into closing lookup")
	}
}

func writeSourceFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func runSessionOutput(t *testing.T, manager *Manager, sessionID SessionID) string {
	t.Helper()

	created, err := manager.CreateExecution(context.Background(), sessionID, nil, ExecutionOptions{})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	terminal, _ := runAndObserve(t, manager, created.ID)
	if terminal.State != StateCompleted || terminal.Output == nil {
		t.Fatalf("terminal execution = %+v, want completed output", terminal)
	}

	return string(terminal.Output.Content)
}
