package ferretapi_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/exec"
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

	if second.Source.Revision <= first.Source.Revision {
		t.Fatalf("source revisions = %d and %d, want increasing revisions",
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

func TestCreateSessionRefreshesCompilationAndUnavailableDiagnostics(t *testing.T) {
	t.Run("compilation failure", func(t *testing.T) {
		fixture := newExecutionFixture(t, "RETURN 1")
		writeSourceFile(t, fixture.workspace.Root(), "query.fql", "RETURN missing")

		_, err := fixture.manager.CreateSession(
			context.Background(),
			fixture.workspace.ID(),
			"query.fql",
		)

		var compilation *exec.CompilationError
		if !errors.As(err, &compilation) || !errors.Is(err, exec.ErrCompilationFailed) {
			t.Fatalf("CreateSession error = %v, want exec.CompilationError", err)
		}

		failedRevision := compilation.Source.Revision
		if failedRevision <= 1 || len(compilation.Diagnostics) == 0 {
			t.Fatalf("exec.CompilationError = %+v, want advanced revision and diagnostics", compilation)
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

	if second.Source.Revision <= first.Source.Revision {
		t.Fatalf("second source revision = %d, want greater than %d",
			second.Source.Revision, first.Source.Revision)
	}

	runtime, err := fixture.manager.CreateDebugRuntime(
		context.Background(),
		first.ID,
		nil,
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}

	defer func() { _ = runtime.Close() }()

	debugSession := runtime.Debugger()
	if _, err := debugSession.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	event, err := debugSession.Continue(context.Background())
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}

	if event.Reason != apidebugger.ReasonCompleted || event.Output == nil || string(event.Output.Content) != "1" {
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
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}

	defer func() { _ = runtime.Close() }()

	event, err := runtime.Debugger().Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if event.Reason != apidebugger.ReasonEntry {
		t.Fatalf("entry event = %+v", event)
	}
}

func TestExecutionWorkingDirectorySelectsSessionFilesystem(t *testing.T) {
	t.Run("unset uses workspace", func(t *testing.T) {
		fixture := newExecutionFixture(t, `RETURN TO_STRING(IO::FS::READ("value.txt"))`)
		if err := os.WriteFile(filepath.Join(fixture.workspace.Root(), "value.txt"), []byte("workspace"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		if created.Options.WorkingDirectory != "" {
			t.Fatalf(
				"working directory = %q, want absent",
				created.Options.WorkingDirectory,
			)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		if terminal.State != exec.StateCompleted || terminal.Output == nil ||
			string(terminal.Output.Content) != `"workspace"` {
			t.Fatalf("terminal = %+v, want workspace output", terminal)
		}
	})

	t.Run("override outside workspace", func(t *testing.T) {
		fixture := newExecutionFixture(t, `RETURN TO_STRING(IO::FS::READ("value.txt"))`)

		runtimeRoot := filepath.Join(t.TempDir(), "runtime root ü")
		if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}

		if err := os.WriteFile(filepath.Join(runtimeRoot, "value.txt"), []byte("runtime"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		canonicalRuntimeRoot, err := filepath.EvalSymlinks(runtimeRoot)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}

		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{WorkingDirectory: runtimeRoot},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		if created.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
			t.Fatalf(
				"working directory = %q, want %q",
				created.Options.WorkingDirectory,
				canonicalRuntimeRoot,
			)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		if terminal.State != exec.StateCompleted || terminal.Output == nil ||
			string(terminal.Output.Content) != `"runtime"` {
			t.Fatalf("terminal = %+v, want runtime output", terminal)
		}
	})

	t.Run("writes stay under override", func(t *testing.T) {
		fixture := newExecutionFixture(t, `RETURN IO::FS::WRITE("created.txt", TO_BINARY("session"))`)
		runtimeRoot := t.TempDir()

		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{WorkingDirectory: runtimeRoot},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		if terminal.State != exec.StateCompleted {
			t.Fatalf("terminal = %+v, want completed", terminal)
		}

		content, err := os.ReadFile(filepath.Join(runtimeRoot, "created.txt"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		if string(content) != "session" {
			t.Fatalf("created content = %q, want session", content)
		}

		if _, err := os.Stat(filepath.Join(fixture.workspace.Root(), "created.txt")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("workspace created file stat error = %v, want not-exist", err)
		}
	})
}

func TestConcurrentExecutionsUseIndependentWorkingDirectories(t *testing.T) {
	fixture := newExecutionFixture(t, `RETURN TO_STRING(IO::FS::READ("value.txt"))`)
	roots := []string{t.TempDir(), t.TempDir()}
	for index, root := range roots {
		if err := os.WriteFile(
			filepath.Join(root, "value.txt"),
			[]byte(strconv.Itoa(index+1)),
			0o600,
		); err != nil {
			t.Fatalf("WriteFile(%d): %v", index, err)
		}
	}

	created := make([]exec.ExecutionSnapshot, len(roots))
	for index, root := range roots {
		var err error

		created[index], err = fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{WorkingDirectory: root},
		)
		if err != nil {
			t.Fatalf("CreateExecution(%d): %v", index, err)
		}
	}

	var wait sync.WaitGroup
	results := make([]exec.ExecutionSnapshot, len(created))
	for index := range created {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index], _ = runAndObserve(t, fixture.manager, created[index].ID)
		}()
	}

	wait.Wait()

	for index, result := range results {
		want := `"` + strconv.Itoa(index+1) + `"`
		if result.State != exec.StateCompleted || result.Output == nil || string(result.Output.Content) != want {
			t.Fatalf("result[%d] = %+v, want %s", index, result, want)
		}
	}
}

func TestWorkingDirectoryRemovedBeforeRunFailsSessionCreation(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")

	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		exec.RuntimeOptions{WorkingDirectory: runtimeRoot},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	if err := os.Remove(runtimeRoot); err != nil {
		t.Fatalf("Remove working directory: %v", err)
	}

	terminal, _ := runAndObserve(t, fixture.manager, created.ID)
	assertFailure(t, terminal, exec.FailureSessionCreation, false)
}

func TestExecutionRuntimeDiagnostics(t *testing.T) {
	t.Run("runtime", func(t *testing.T) {
		fixture := newExecutionFixture(t, "RETURN 1 / @zero")

		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			map[string]any{"zero": 0},
			exec.RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		assertFailure(t, terminal, exec.FailureRuntime, false)

		if got := terminal.Failure.Diagnostics; len(got) != 1 ||
			got[0].Code == "" || !strings.Contains(got[0].Message, "division by zero") ||
			got[0].Range.Start == got[0].Range.End {
			t.Fatalf("runtime diagnostics = %+v, want one source-located division-by-zero diagnostic", got)
		}
	})

	t.Run("aggregate runtime diagnostics", func(t *testing.T) {
		fixture := newExecutionFixture(t, `LET first = @first
LET second = @second
LET third = @third
RETURN [first, second, third]`)

		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		assertFailure(t, terminal, exec.FailureRuntime, false)

		if got, want := terminal.Failure.Message, "Found 3 errors"; got != want {
			t.Fatalf("failure message = %q, want %q", got, want)
		}

		diagnostics := terminal.Failure.Diagnostics
		if len(diagnostics) != 3 {
			t.Fatalf("runtime diagnostics = %+v, want three missing-parameter diagnostics", diagnostics)
		}

		for i, name := range []string{"@first", "@second", "@third"} {
			diagnostic := diagnostics[i]
			if diagnostic.Code == "" || !strings.Contains(diagnostic.Message, "missing parameter") ||
				!strings.Contains(diagnostic.Message, name) || diagnostic.Range.Start.Line != uint32(i) ||
				diagnostic.Range.Start == diagnostic.Range.End {
				t.Fatalf("runtime diagnostic[%d] = %+v, want source-located diagnostic for %s", i, diagnostic, name)
			}
		}
	})
}
