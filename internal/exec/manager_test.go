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

func TestNewRequiresWorkspaceManager(t *testing.T) {
	manager, err := New(nil)
	if manager != nil {
		t.Fatal("New returned a manager for a nil workspace dependency")
	}
	if !errors.Is(err, errNilWorkspaceManager) {
		t.Fatalf("New error = %v, want %v", err, errNilWorkspaceManager)
	}
}

func TestManagerDoesNotOwnWorkspaceManager(t *testing.T) {
	workspaces := workspace.New()
	opened, err := workspaces.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })

	manager, err := New(workspaces)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if manager.workspaces != workspaces {
		t.Fatal("New did not retain the supplied workspace manager")
	}

	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := workspaces.Get(context.Background(), opened.ID()); err != nil {
		t.Fatalf("workspace after execution manager Close: %v", err)
	}
}

func TestManagerCloseSettlesMultipleWorkspaceGroups(t *testing.T) {
	ctx := context.Background()
	roots := [2]string{t.TempDir(), t.TempDir()}
	workspaces := workspace.New()
	manager := mustNewManager(t, workspaces)
	// Register after TempDir so Windows releases rooted filesystem handles before directory cleanup.
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	type resources struct {
		session   SessionSnapshot
		execution ExecutionSnapshot
	}
	created := make([]resources, 0, 2)
	for _, root := range roots {
		writeSourceFile(t, root, "query.fql", "RETURN 1")
		opened, err := workspaces.Open(ctx, root)
		if err != nil {
			t.Fatalf("workspace Open: %v", err)
		}
		session, err := manager.CreateSession(ctx, opened.ID(), "query.fql")
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		execution, err := manager.CreateExecution(ctx, session.ID, nil, RuntimeOptions{})
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
		created = append(created, resources{session: session, execution: execution})
	}

	if err := manager.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, item := range created {
		if _, err := manager.GetSession(ctx, item.session.ID); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("GetSession after Close error = %v, want ErrSessionNotFound", err)
		}
		if _, err := manager.GetExecution(ctx, item.execution.ID); !errors.Is(err, ErrExecutionNotFound) {
			t.Fatalf("GetExecution after Close error = %v, want ErrExecutionNotFound", err)
		}
	}

	manager.sessions.mu.RLock()
	groups := len(manager.sessions.groups)
	manager.sessions.mu.RUnlock()
	if groups != 0 {
		t.Fatalf("workspace groups after Close = %d, want 0", groups)
	}
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}

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

func TestCreateSessionDiscoversSourceCreatedAfterWorkspaceOpen(t *testing.T) {
	root := t.TempDir()
	workspaces := workspace.New()
	manager := mustNewManager(t, workspaces)
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("workspace Open: %v", err)
	}
	writeSourceFile(t, root, "created.fql", "RETURN 42")

	session, err := manager.CreateSession(context.Background(), opened.ID(), "created.fql")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.Source.RelativePath != "created.fql" || runSessionOutput(t, manager, session.ID) != "42" {
		t.Fatalf("created source Session = %+v", session)
	}
}

func TestCreateSessionDefensiveDiscoveryEnforcesWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	workspaces := workspace.New()
	manager := mustNewManager(t, workspaces)
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("workspace Open: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "testdata"), 0o700); err != nil {
		t.Fatalf("MkdirAll testdata: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatalf("MkdirAll nested: %v", err)
	}
	writeSourceFile(t, root, "testdata/ignored.fql", "RETURN 1")
	writeSourceFile(t, root, "nested/query.fql", "RETURN 2")
	writeSourceFile(t, root, "nested/go.mod", "module example.com/nested")
	if err := os.Mkdir(filepath.Join(root, "directory.fql"), 0o700); err != nil {
		t.Fatalf("Mkdir directory.fql: %v", err)
	}

	outside := t.TempDir()
	writeSourceFile(t, outside, "outside.fql", "RETURN 3")
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}

	paths := []string{
		"testdata/ignored.fql",
		"nested/query.fql",
		"linked/outside.fql",
		"directory.fql",
		"../outside.fql",
		"missing.fql",
		"notes.txt",
	}
	for _, relativePath := range paths {
		t.Run(relativePath, func(t *testing.T) {
			if _, err := manager.CreateSession(context.Background(), opened.ID(), relativePath); !errors.Is(err, workspace.ErrDocumentNotFound) {
				t.Fatalf("CreateSession error = %v, want ErrDocumentNotFound", err)
			}
		})
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
		failedRevision := compilation.Source.Revision
		if failedRevision <= 1 || len(compilation.Diagnostics) == 0 {
			t.Fatalf("CompilationError = %+v, want advanced revision and diagnostics", compilation)
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
		if recovered.Source.Revision <= failedRevision || runSessionOutput(t, fixture.manager, recovered.ID) != "3" {
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
		if !errors.Is(err, workspace.ErrDocumentNotFound) {
			t.Fatalf("CreateSession error = %v, want ErrDocumentNotFound", err)
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

	runtime, err := fixture.manager.CreateDebugRuntime(
		context.Background(),
		first.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}
	defer func() { _ = runtime.Close() }()
	if runtime.runtime.target.source != first.Source || runtime.runtime.target.text != "LET value = 1\nRETURN value" {
		t.Fatalf("debug runtime = source %+v text %q, want first Session snapshot",
			runtime.runtime.target.source, runtime.runtime.target.text)
	}

	debugSession := runtime.Debugger()
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

func TestOldSessionLazilyCompilesDebugPlanAfterSourceRemoval(t *testing.T) {
	fixture := newExecutionFixture(t, "LET value = 1\nRETURN value")
	first := fixture.session
	if err := os.Remove(filepath.Join(fixture.workspace.Root(), "query.fql")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fixture.workspace.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, workspace.ErrDocumentNotFound) {
		t.Fatalf("RefreshDocument error = %v, want ErrDocumentNotFound", err)
	}
	if output := runSessionOutput(t, fixture.manager, first.ID); output != "1" {
		t.Fatalf("retained Session output = %q, want 1", output)
	}

	runtime, err := fixture.manager.CreateDebugRuntime(
		context.Background(),
		first.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	event, err := runtime.Debugger().Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if event.Reason != ferret.DebugReasonEntry {
		t.Fatalf("entry event = %+v", event)
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
	manager := mustNewManager(t, workspaces)
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

	manager.sessions.mu.RLock()
	registered := len(manager.sessions.entries)
	manager.sessions.mu.RUnlock()
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

func TestCloseSessionRequiresContext(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	assertPanics(t, func() {
		//lint:ignore SA1012 This test verifies that a required context cannot be nil.
		_ = fixture.manager.CloseSession(nil, fixture.session.ID)
	})
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
	manager := mustNewManager(t, workspace.New())
	workspaceID := workspace.ID("workspace")
	creation, err := manager.sessions.beginCreate(workspaceID)
	if err != nil {
		t.Fatalf("begin Session creation: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.CloseWorkspace(ctx, workspaceID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseWorkspace error = %v, want context.Canceled", err)
	}
	if _, err := manager.sessions.beginCreate(workspaceID); !errors.Is(err, workspace.ErrClosed) {
		t.Fatalf("begin Session creation during workspace close error = %v, want workspace.ErrClosed", err)
	}
	manager.sessions.finishCreate(creation)
	if err := manager.CloseWorkspace(context.Background(), workspaceID); err != nil {
		t.Fatalf("wait for CloseWorkspace: %v", err)
	}
	manager.sessions.mu.RLock()
	_, retained := manager.sessions.groups[workspaceID]
	manager.sessions.mu.RUnlock()
	if retained {
		t.Fatal("workspace group retained after committed close")
	}
}

func TestSessionCloseCollectsExecutionAdmittedBeforeClose(t *testing.T) {
	manager, snapshot, _ := newHookedManager(t, "RETURN 1")
	closed := make(chan error, 1)
	var admitted *execution
	func() {
		creation, err := manager.sessions.beginRuntimeCreate(snapshot.ID)
		if err != nil {
			t.Fatalf("begin Execution creation: %v", err)
		}
		defer creation.finish()

		parent := creation.session()
		go func() {
			closed <- manager.CloseSession(context.Background(), snapshot.ID)
		}()
		waitForSessionClosing(t, parent)

		select {
		case err := <-closed:
			t.Fatalf("CloseSession completed with admitted Execution creator: %v", err)
		default:
		}

		if _, err := manager.CreateExecution(
			context.Background(),
			snapshot.ID,
			nil,
			RuntimeOptions{},
		); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("CreateExecution after Session close error = %v, want ErrSessionNotFound", err)
		}

		input, err := newRuntimeInput(nil, RuntimeOptions{})
		if err != nil {
			t.Fatalf("prepare runtime input: %v", err)
		}
		admitted = newExecution(
			ExecutionID("admitted-execution"),
			newExecutionRuntime(parent.runtimeTarget(), input),
		)
		manager.executions.add(admitted)
	}()

	if err := <-closed; err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := manager.GetExecution(
		context.Background(),
		admitted.id,
	); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("GetExecution after parent close error = %v, want ErrExecutionNotFound", err)
	}

	manager.executions.mu.RLock()
	entries := len(manager.executions.entries)
	groups := len(manager.executions.bySession)
	manager.executions.mu.RUnlock()
	if entries != 0 || groups != 0 {
		t.Fatalf("Execution registry after parent close = %d entries, %d groups", entries, groups)
	}
}

func TestManagerCloseWaitsForAdmittedSessionCreation(t *testing.T) {
	manager := mustNewManager(t, workspace.New())
	workspaceID := workspace.ID("workspace")
	creation, err := manager.sessions.beginCreate(workspaceID)
	if err != nil {
		t.Fatalf("begin Session creation: %v", err)
	}

	closeContext := newObservedDoneContext()
	closed := make(chan error, 1)
	go func() {
		closed <- manager.Close(closeContext)
	}()
	waitForSignal(t, closeContext.observed, "Manager close wait")

	if _, err := manager.sessions.beginCreate(workspace.ID("other")); !errors.Is(err, ErrClosed) {
		t.Fatalf("begin Session creation after Manager close error = %v, want ErrClosed", err)
	}
	manager.sessions.finishCreate(creation)

	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}

	manager.sessions.mu.RLock()
	entries := len(manager.sessions.entries)
	groups := len(manager.sessions.groups)
	manager.sessions.mu.RUnlock()
	if entries != 0 || groups != 0 {
		t.Fatalf("Session registry after Manager close = %d entries, %d groups", entries, groups)
	}
}

func TestParentCloseDoesNotReadoptSettledExecution(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	if err := fixture.manager.CloseExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("CloseExecution: %v", err)
	}
	if children := fixture.manager.executions.beginSessionClose(fixture.session.ID); len(children) != 0 {
		t.Fatalf("settled Executions retained by Session = %d, want 0", len(children))
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

	created, err := manager.CreateExecution(context.Background(), sessionID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	terminal, _ := runAndObserve(t, manager, created.ID)
	if terminal.State != StateCompleted || terminal.Output == nil {
		t.Fatalf("terminal execution = %+v, want completed output", terminal)
	}

	return string(terminal.Output.Content)
}
