package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/MontFerret/ferret/v2"
)

func TestExecutionLifecycleParametersAndRunOnce(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN @value")
	input := map[string]any{
		"value":  7,
		"nested": map[string]any{"items": []any{"one", "two"}},
	}
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		input,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if created.State != StateCreated || created.Options.OutputContentType != "application/json" {
		t.Fatalf("created = %+v", created)
	}
	if _, err := uuid.Parse(string(created.ID)); err != nil {
		t.Fatalf("Execution ID is not a UUID: %v", err)
	}

	input["value"] = 99
	input["nested"].(map[string]any)["items"].([]any)[0] = "mutated"
	created.Parameters["value"] = 100
	stored, err := fixture.manager.GetExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if stored.Parameters["value"] != 7 ||
		stored.Parameters["nested"].(map[string]any)["items"].([]any)[0] != "one" {
		t.Fatalf("stored parameters = %#v, want immutable copy", stored.Parameters)
	}

	terminal, events := runAndObserve(t, fixture.manager, created.ID)
	if terminal.State != StateCompleted || terminal.Output == nil ||
		terminal.Output.ContentType != "application/json" || string(terminal.Output.Content) != "7" {
		t.Fatalf("terminal = %+v", terminal)
	}
	assertLifecycleEvents(t, events, created.ID, []EventKind{EventCreated, EventStarted, EventCompleted})
	if _, err := fixture.manager.RunExecution(context.Background(), created.ID); !errors.Is(err, ErrExecutionTerminal) {
		t.Fatalf("second RunExecution error = %v, want ErrExecutionTerminal", err)
	}
}

func TestConcurrentExecutionsFromOneSessionAreIsolated(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN @value")
	values := []int{1, 2, 3, 4}
	created := make([]ExecutionSnapshot, len(values))
	for i, value := range values {
		var err error
		created[i], err = fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			map[string]any{"value": value},
			RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution(%d): %v", value, err)
		}
	}

	var wait sync.WaitGroup
	results := make([]ExecutionSnapshot, len(created))
	for i := range created {
		wait.Add(1)
		go func() {
			defer wait.Done()

			results[i], _ = runAndObserve(t, fixture.manager, created[i].ID)
		}()
	}
	wait.Wait()

	for i, result := range results {
		if result.State != StateCompleted || result.Output == nil ||
			string(result.Output.Content) != strconv.Itoa(i+1) {
			t.Fatalf("result[%d] = %+v", i, result)
		}
	}
}

func TestExecutionWorkingDirectorySelectsSessionFilesystem(t *testing.T) {
	t.Run("absent uses workspace", func(t *testing.T) {
		fixture := newExecutionFixture(t, `RETURN TO_STRING(IO::FS::READ("value.txt"))`)
		if err := os.WriteFile(filepath.Join(fixture.workspace.Root(), "value.txt"), []byte("workspace"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
		if created.Options.WorkingDirectory != nil {
			t.Fatalf("WorkingDirectory = %v, want absent", created.Options.WorkingDirectory)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		if terminal.State != StateCompleted || terminal.Output == nil ||
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
			RuntimeOptions{WorkingDirectory: workingDirectoryPointer(runtimeRoot)},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
		if created.Options.WorkingDirectory == nil ||
			*created.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
			t.Fatalf("WorkingDirectory = %v, want %q", created.Options.WorkingDirectory, canonicalRuntimeRoot)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		if terminal.State != StateCompleted || terminal.Output == nil ||
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
			RuntimeOptions{WorkingDirectory: workingDirectoryPointer(runtimeRoot)},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		if terminal.State != StateCompleted {
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

	created := make([]ExecutionSnapshot, len(roots))
	for index, root := range roots {
		var err error
		created[index], err = fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			RuntimeOptions{WorkingDirectory: workingDirectoryPointer(root)},
		)
		if err != nil {
			t.Fatalf("CreateExecution(%d): %v", index, err)
		}
	}

	var wait sync.WaitGroup
	results := make([]ExecutionSnapshot, len(created))
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
		if result.State != StateCompleted || result.Output == nil || string(result.Output.Content) != want {
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
		RuntimeOptions{WorkingDirectory: workingDirectoryPointer(runtimeRoot)},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := os.Remove(runtimeRoot); err != nil {
		t.Fatalf("Remove working directory: %v", err)
	}

	terminal, _ := runAndObserve(t, fixture.manager, created.ID)
	assertFailure(t, terminal, FailureSessionCreation, false)
}

func TestRepeatedExecutionsDoNotRecompileSessionPlan(t *testing.T) {
	var compilations atomic.Int64
	manager, session, _ := newHookedManager(t, "RETURN @value", ferret.WithBeforeCompileHook(
		func(context.Context) error {
			compilations.Add(1)

			return nil
		},
	))

	for value := 1; value <= 4; value++ {
		created, err := manager.CreateExecution(
			context.Background(),
			session.ID,
			map[string]any{"value": value},
			RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
		terminal, _ := runAndObserve(t, manager, created.ID)
		if terminal.State != StateCompleted {
			t.Fatalf("terminal = %+v", terminal)
		}
	}
	if got := compilations.Load(); got != 1 {
		t.Fatalf("compilations = %d, want exactly the Session compilation", got)
	}
}

func TestExecutionFailureCategoriesAndPartialOutput(t *testing.T) {
	t.Run("session creation", func(t *testing.T) {
		manager, session, _ := newHookedManager(t, "RETURN 1")
		created, err := manager.CreateExecution(context.Background(), session.ID, nil, RuntimeOptions{})
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		plan := retainedSession(t, manager, session.ID).session.plan
		if err := plan.Close(); err != nil {
			t.Fatalf("Plan.Close: %v", err)
		}

		terminal, _ := runAndObserve(t, manager, created.ID)
		assertFailure(t, terminal, FailureSessionCreation, false)
	})

	t.Run("runtime", func(t *testing.T) {
		fixture := newExecutionFixture(t, "RETURN 1 / @zero")
		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			map[string]any{"zero": 0},
			RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		assertFailure(t, terminal, FailureRuntime, false)
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
			RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, fixture.manager, created.ID)
		assertFailure(t, terminal, FailureRuntime, false)
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

	t.Run("cleanup with output", func(t *testing.T) {
		want := errors.New("cleanup failed")
		manager, session, _ := newHookedManager(t, "RETURN 1", ferret.WithSessionCloseHook(func() error {
			return want
		}))
		created, err := manager.CreateExecution(context.Background(), session.ID, nil, RuntimeOptions{})
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}

		terminal, _ := runAndObserve(t, manager, created.ID)
		assertFailure(t, terminal, FailureCleanup, true)
		if string(terminal.Output.Content) != "1" {
			t.Fatalf("partial output = %q, want 1", terminal.Output.Content)
		}
	})
}

func TestExecutionCancellationBeforeAndDuringRun(t *testing.T) {
	t.Run("before run", func(t *testing.T) {
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

		cancelled, err := fixture.manager.CancelExecution(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("CancelExecution: %v", err)
		}
		if cancelled.State != StateCancelled {
			t.Fatalf("state = %v, want cancelled", cancelled.State)
		}
		if _, err := fixture.manager.RunExecution(context.Background(), created.ID); !errors.Is(err, ErrExecutionTerminal) {
			t.Fatalf("RunExecution error = %v, want terminal", err)
		}
	})

	t.Run("during run", func(t *testing.T) {
		fixture := newExecutionFixture(t, "RETURN WAITFOR FALSE TIMEOUT 30s EVERY 10ms")
		created, err := fixture.manager.CreateExecution(
			context.Background(),
			fixture.session.ID,
			nil,
			RuntimeOptions{WorkingDirectory: workingDirectoryPointer(t.TempDir())},
		)
		if err != nil {
			t.Fatalf("CreateExecution: %v", err)
		}
		subscription, err := fixture.manager.WatchExecution(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("WatchExecution: %v", err)
		}
		defer subscription.Cancel()
		if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
			t.Fatalf("RunExecution: %v", err)
		}
		started := <-subscription.Events
		if started.Kind != EventStarted {
			t.Fatalf("event = %+v, want started", started)
		}

		var wait sync.WaitGroup
		for range 8 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, _ = fixture.manager.CancelExecution(context.Background(), created.ID)
			}()
		}
		wait.Wait()
		terminal := <-subscription.Events
		if terminal.Kind != EventCancelled || terminal.Snapshot.State != StateCancelled {
			t.Fatalf("terminal event = %+v, want cancelled", terminal)
		}
		if _, ok := <-subscription.Events; ok {
			t.Fatal("received more than one terminal event")
		}
	})
}

func TestSessionRefreshDoesNotCancelActiveExecution(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN WAITFOR FALSE TIMEOUT 30s EVERY 10ms")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	subscription, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	defer subscription.Cancel()
	if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if started := <-subscription.Events; started.Kind != EventStarted {
		t.Fatalf("event = %+v, want started", started)
	}

	writeSourceFile(t, fixture.workspace.Root(), "query.fql", "RETURN 2")
	refreshed, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.workspace.ID(),
		"query.fql",
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if refreshed.Source.Revision <= fixture.session.Source.Revision {
		t.Fatalf("refreshed source revision = %d, want greater than %d",
			refreshed.Source.Revision, fixture.session.Source.Revision)
	}
	if output := runSessionOutput(t, fixture.manager, refreshed.ID); output != "2" {
		t.Fatalf("refreshed Session output = %q, want 2", output)
	}
	active, err := fixture.manager.GetExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if active.State != StateRunning {
		t.Fatalf("original execution state = %v, want running", active.State)
	}

	if _, err := fixture.manager.CancelExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("CancelExecution: %v", err)
	}
	if terminal := <-subscription.Events; terminal.Kind != EventCancelled {
		t.Fatalf("terminal event = %+v, want cancelled", terminal)
	}
}

func TestCancellationRacingSuccessNeverOverwritesTerminalState(t *testing.T) {
	for range 20 {
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
		subscription, err := fixture.manager.WatchExecution(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("WatchExecution: %v", err)
		}
		if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
			t.Fatalf("RunExecution: %v", err)
		}
		_, _ = fixture.manager.CancelExecution(context.Background(), created.ID)

		var terminal Event
		for event := range subscription.Events {
			terminal = event
		}
		subscription.Cancel()
		if terminal.Kind != EventCompleted && terminal.Kind != EventCancelled {
			t.Fatalf("terminal event = %+v", terminal)
		}
		after, err := fixture.manager.CancelExecution(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("Cancel after terminal: %v", err)
		}
		if after.State != terminal.Snapshot.State {
			t.Fatalf("state overwritten: terminal=%v after=%v", terminal.Snapshot.State, after.State)
		}
	}
}

func TestSessionAndWorkspaceCloseCascadeExecutions(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN WAITFOR FALSE TIMEOUT 30s EVERY 10ms")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	subscription, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	defer subscription.Cancel()
	if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if event := <-subscription.Events; event.Kind != EventStarted {
		t.Fatalf("event = %+v, want started", event)
	}

	if err := fixture.workspaces.Close(context.Background(), fixture.workspace.ID()); err != nil {
		t.Fatalf("workspace Close: %v", err)
	}
	terminal := <-subscription.Events
	if terminal.Kind != EventCancelled {
		t.Fatalf("terminal = %+v, want cancelled", terminal)
	}
	if _, err := fixture.manager.GetSession(context.Background(), fixture.session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
	if _, err := fixture.manager.GetExecution(context.Background(), created.ID); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("GetExecution error = %v", err)
	}
}

func TestCloseExecutionCancelsRunningAndEndsWatch(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN WAITFOR FALSE TIMEOUT 30s EVERY 10ms")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	subscription, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	defer subscription.Cancel()
	if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if event := <-subscription.Events; event.Kind != EventStarted {
		t.Fatalf("event = %+v, want started", event)
	}

	if err := fixture.manager.CloseExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("CloseExecution: %v", err)
	}
	if terminal := <-subscription.Events; terminal.Kind != EventCancelled {
		t.Fatalf("terminal = %+v, want cancelled", terminal)
	}
	if _, ok := <-subscription.Events; ok {
		t.Fatal("watch remained open after CloseExecution")
	}
	if _, err := fixture.manager.GetExecution(context.Background(), created.ID); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("GetExecution error = %v, want ErrExecutionNotFound", err)
	}
}

func TestManagerCloseCascadesRunningExecution(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN WAITFOR FALSE TIMEOUT 30s EVERY 10ms")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	subscription, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	defer subscription.Cancel()
	if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if event := <-subscription.Events; event.Kind != EventStarted {
		t.Fatalf("event = %+v, want started", event)
	}

	if err := fixture.manager.Close(context.Background()); err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}
	if terminal := <-subscription.Events; terminal.Kind != EventCancelled {
		t.Fatalf("terminal = %+v, want cancelled", terminal)
	}
	if _, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.workspace.ID(),
		"query.fql",
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("CreateSession after Close error = %v, want ErrClosed", err)
	}
}

func TestInvalidParametersAndUnknownCloseContracts(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	if _, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		map[string]any{"invalid": make(chan int)},
		RuntimeOptions{},
	); !errors.Is(err, ErrInvalidParameters) {
		t.Fatalf("CreateExecution error = %v, want ErrInvalidParameters", err)
	}
	if _, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{WorkingDirectory: workingDirectoryPointer(filepath.Join(t.TempDir(), "missing"))},
	); !errors.Is(err, ErrInvalidExecutionOptions) {
		t.Fatalf("CreateExecution error = %v, want ErrInvalidExecutionOptions", err)
	}
	if err := fixture.manager.CloseExecution(context.Background(), "missing"); err != nil {
		t.Fatalf("CloseExecution missing: %v", err)
	}
	if err := fixture.manager.CloseSession(context.Background(), "missing"); err != nil {
		t.Fatalf("CloseSession missing: %v", err)
	}
}

func assertLifecycleEvents(t *testing.T, events []Event, id ExecutionID, want []EventKind) {
	t.Helper()

	if len(events) != len(want) {
		t.Fatalf("events = %+v, want kinds %+v", events, want)
	}
	for i, event := range events {
		if event.Execution != id || event.Snapshot.ID != id || event.Sequence != uint64(i+1) ||
			event.Kind != want[i] {
			t.Fatalf("event[%d] = %+v", i, event)
		}
	}
}

func assertFailure(t *testing.T, terminal ExecutionSnapshot, category FailureCategory, wantOutput bool) {
	t.Helper()

	if terminal.State != StateFailed || terminal.Failure == nil || terminal.Failure.Category != category ||
		terminal.Failure.Message == "" {
		t.Fatalf("terminal = %+v, want failed category %v", terminal, category)
	}
	if (terminal.Output != nil) != wantOutput {
		t.Fatalf("output = %+v, want present %t", terminal.Output, wantOutput)
	}
}
