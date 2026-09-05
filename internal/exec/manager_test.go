package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/api"
	apidiagnostics "github.com/MontFerret/api/diagnostics"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestNewRequiresDependencies(t *testing.T) {
	runtime := newRuntimeSpy()
	tests := []struct {
		name       string
		workspaces *workspace.Manager
		runtime    api.Runtime
		want       error
	}{
		{name: "workspace manager", runtime: runtime, want: errNilWorkspaceManager},
		{name: "runtime", workspaces: workspace.New(), want: errNilRuntime},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, err := New(test.workspaces, test.runtime)
			if manager != nil {
				t.Fatal("New returned a manager with a missing dependency")
			}

			if !errors.Is(err, test.want) {
				t.Fatalf("New error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestManagerDoesNotOwnWorkspaceManager(t *testing.T) {
	workspaces := workspace.New()

	opened, err := workspaces.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })

	runtime := newRuntimeSpy()

	manager, err := New(workspaces, runtime)
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

	if runtime.closeCalls.Load() != 0 {
		t.Fatalf("runtime close calls = %d, want 0", runtime.closeCalls.Load())
	}
}

func TestManagerCompilesRetainedSourceAndAppliesUniversalSessionOptions(t *testing.T) {
	root := t.TempDir()

	sourcePath := filepath.Join(root, "query.fql")
	if err := os.WriteFile(sourcePath, []byte("RETURN @value"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	workspaces := workspace.New()

	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	runtime := newRuntimeSpy()
	runtime.run = parameterOutput

	manager, err := New(workspaces, runtime)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	snapshot, err := manager.CreateSession(context.Background(), opened.ID(), "query.fql")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	compiled, debugCompiled := runtime.sources()
	if len(compiled) != 1 || compiled[0].Name != sourcePath || compiled[0].Content != "RETURN @value" {
		t.Fatalf("compiled sources = %+v", compiled)
	}

	if len(debugCompiled) != 0 {
		t.Fatalf("debug sources before debug creation = %+v", debugCompiled)
	}

	created, err := manager.CreateExecution(
		context.Background(),
		snapshot.ID,
		Parameters{"value": 7},
		RuntimeOptions{OutputContentType: "application/json"},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	terminal, _ := runAndObserve(t, manager, created.ID)
	if terminal.State != StateCompleted || terminal.Output == nil || string(terminal.Output.Content) != "7" {
		t.Fatalf("terminal = %+v", terminal)
	}

	plan := retainedSession(t, manager, snapshot.ID).session.plan.(*planSpy)
	plan.mu.Lock()
	options := plan.lastOptions
	plan.mu.Unlock()

	if options.params["value"] != 7 || options.contentType != "application/json" || options.fsRoot != root {
		t.Fatalf("session options = %+v, want parameters, content type, and workspace root", options)
	}

	override := t.TempDir()

	canonicalOverride, err := filepath.EvalSymlinks(override)
	if err != nil {
		t.Fatalf("EvalSymlinks override: %v", err)
	}

	created, err = manager.CreateExecution(
		context.Background(),
		snapshot.ID,
		Parameters{"value": 8},
		RuntimeOptions{WorkingDirectory: override},
	)
	if err != nil {
		t.Fatalf("CreateExecution override: %v", err)
	}

	if terminal, _ = runAndObserve(t, manager, created.ID); terminal.State != StateCompleted {
		t.Fatalf("override terminal = %+v", terminal)
	}

	plan.mu.Lock()
	overrideOptions := plan.lastOptions
	plan.mu.Unlock()

	if overrideOptions.fsRoot != canonicalOverride {
		t.Fatalf("override FS root = %q, want %q", overrideOptions.fsRoot, canonicalOverride)
	}

	debugRuntime, err := manager.CreateDebugRuntime(
		context.Background(),
		snapshot.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}

	if err := debugRuntime.Close(); err != nil {
		t.Fatalf("DebugRuntime.Close: %v", err)
	}

	compiled, debugCompiled = runtime.sources()
	if len(compiled) != 1 || len(debugCompiled) != 1 || debugCompiled[0] != compiled[0] {
		t.Fatalf("compile sources = normal %+v, debug %+v", compiled, debugCompiled)
	}
}

func TestManagerClosesPlanReturnedWithCompileFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	workspaces := workspace.New()

	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	runtime := newRuntimeSpy()
	failedPlan := &planSpy{runtime: runtime}
	want := errors.New("compile failed")
	runtime.compilePlan = failedPlan
	runtime.compileErr = want

	manager, err := New(workspaces, runtime)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })

	_, err = manager.CreateSession(context.Background(), opened.ID(), "query.fql")

	var compilation *CompilationError
	if !errors.Is(err, want) || !errors.As(err, &compilation) {
		t.Fatalf("CreateSession error = %v, want CompilationError preserving cause", err)
	}

	failedPlan.mu.Lock()
	closed := failedPlan.closed
	failedPlan.mu.Unlock()

	if !closed {
		t.Fatal("plan returned with compile failure was not closed")
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

func TestCreateSessionReturnsStructuredDiagnosticsWithoutRegistration(t *testing.T) {
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN missing")
	workspaces := workspace.New()

	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	runtime := newRuntimeSpy()
	runtime.compileErr = apidiagnostics.Diagnostics{{
		Kind:    apidiagnostics.UnexpectedError,
		Message: "unknown variable",
	}}
	manager := newManagerWithRuntime(t, workspaces, runtime)
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
	manager, session, _ := newHookedManager(t, "RETURN 1", withPlanCloseHook(func() error {
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
		//nolint:staticcheck // Exercise the nil-context rejection contract.
		_ = fixture.manager.CloseSession(nil, fixture.session.ID)
	})
}

func TestSessionCloseCallerTimeoutDoesNotStopCleanup(t *testing.T) {
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	manager, session, _ := newHookedManager(t, "RETURN 1", withPlanCloseHook(func() error {
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
