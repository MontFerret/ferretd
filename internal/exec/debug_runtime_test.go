package exec

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/ferret/v2"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestCreateDebugRuntimeCoordinatesCachesAndRetries(t *testing.T) {
	t.Run("concurrent_success", func(t *testing.T) {
		manager, snapshot, engine := newHookedManager(t, "RETURN 1")
		parent := retainedSession(t, manager, snapshot.ID).session
		var calls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		parent.compileDebug = func(context.Context) (workspace.Compilation, error) {
			if calls.Add(1) == 1 {
				close(started)
			}

			<-release
			plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))

			return workspace.Compilation{Plan: plan, Source: parent.source}, err
		}

		var wait sync.WaitGroup
		errorsChannel := make(chan error, 2)
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()

				runtime, err := manager.CreateDebugRuntime(
					context.Background(),
					snapshot.ID,
					nil,
					RuntimeOptions{},
				)
				if runtime != nil {
					err = errors.Join(err, runtime.Close())
				}

				errorsChannel <- err
			}()
		}

		<-started
		close(release)
		wait.Wait()
		close(errorsChannel)

		for err := range errorsChannel {
			if err != nil {
				t.Fatalf("CreateDebugRuntime: %v", err)
			}
		}

		if calls.Load() != 1 {
			t.Fatalf("debug compile calls = %d, want 1", calls.Load())
		}
	})

	t.Run("failure_cache_and_cancellation_retry", func(t *testing.T) {
		manager, snapshot, _ := newHookedManager(t, "RETURN 1")
		parent := retainedSession(t, manager, snapshot.ID).session
		compileErr := errors.New("deterministic compile failure")
		var calls atomic.Int32
		started := make(chan struct{})
		parent.compileDebug = func(ctx context.Context) (workspace.Compilation, error) {
			switch calls.Add(1) {
			case 1:
				close(started)
				<-ctx.Done()

				return workspace.Compilation{}, ctx.Err()
			default:
				return workspace.Compilation{}, compileErr
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := manager.CreateDebugRuntime(ctx, snapshot.ID, nil, RuntimeOptions{})
			result <- err
		}()

		<-started
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled creation error = %v", err)
		}

		if _, err := manager.CreateDebugRuntime(
			context.Background(),
			snapshot.ID,
			nil,
			RuntimeOptions{},
		); !errors.Is(err, compileErr) {
			t.Fatalf("compile error = %v", err)
		}

		if _, err := manager.CreateDebugRuntime(
			context.Background(),
			snapshot.ID,
			nil,
			RuntimeOptions{},
		); !errors.Is(err, compileErr) {
			t.Fatalf("cached compile error = %v", err)
		}

		if calls.Load() != 2 {
			t.Fatalf("debug compile calls = %d, want 2", calls.Load())
		}
	})
}

func TestDebugRuntimePreparesParametersOptionsOutputAndCancellation(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN @value")
	workingDirectory := t.TempDir()
	input := map[string]any{
		"value":  7,
		"nested": map[string]any{"items": []any{"one", "two"}},
	}
	runtime, err := fixture.manager.CreateDebugRuntime(
		context.Background(),
		fixture.session.ID,
		input,
		RuntimeOptions{OutputContentType: " \t\n", WorkingDirectory: workingDirectory},
	)
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	input["value"] = 99
	input["nested"].(map[string]any)["items"].([]any)[0] = "caller"
	parameters := runtime.Parameters()
	parameters["nested"].(map[string]any)["items"].([]any)[1] = "snapshot"
	retained := runtime.Parameters()
	if retained["value"] != 7 ||
		retained["nested"].(map[string]any)["items"].([]any)[0] != "one" ||
		retained["nested"].(map[string]any)["items"].([]any)[1] != "two" {
		t.Fatalf("runtime parameters = %#v, want immutable prepared values", retained)
	}
	if runtime.Options().OutputContentType != defaultOutputContentType {
		t.Fatalf("OutputContentType = %q, want %q", runtime.Options().OutputContentType, defaultOutputContentType)
	}
	canonicalWorkingDirectory, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if runtime.Options().WorkingDirectory != filepath.Clean(canonicalWorkingDirectory) {
		t.Fatalf(
			"WorkingDirectory = %q, want %q",
			runtime.Options().WorkingDirectory,
			canonicalWorkingDirectory,
		)
	}

	if _, err := runtime.Debugger().Start(runtime.Context()); err != nil {
		t.Fatalf("debugger Start: %v", err)
	}
	event, err := runtime.Debugger().Continue(runtime.Context())
	if err != nil {
		t.Fatalf("debugger Continue: %v", err)
	}
	output := runtime.MaterializeOutput(event.Output)
	if output == nil || output.ContentType != defaultOutputContentType || string(output.Content) != "7" {
		t.Fatalf("runtime output = %+v", output)
	}
	event.Output.Content[0] = '9'
	if string(output.Content) != "7" {
		t.Fatalf("runtime output changed with Ferret output: %q", output.Content)
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("DebugRuntime.Close: %v", err)
	}
	if !errors.Is(runtime.Context().Err(), context.Canceled) {
		t.Fatalf("runtime context error = %v, want context.Canceled", runtime.Context().Err())
	}

	if _, err := fixture.manager.CreateDebugRuntime(
		context.Background(),
		fixture.session.ID,
		map[string]any{"invalid": make(chan int)},
		RuntimeOptions{},
	); !errors.Is(err, ErrInvalidParameters) {
		t.Fatalf("invalid parameters error = %v, want ErrInvalidParameters", err)
	}
}

func TestCreateDebugRuntimeRejectsChangedSourceAndCachesFailure(t *testing.T) {
	var closes atomic.Int32
	manager, snapshot, engine := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	var calls atomic.Int32
	parent.compileDebug = func(context.Context) (workspace.Compilation, error) {
		calls.Add(1)
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))
		source := parent.source
		source.Revision++

		return workspace.Compilation{Plan: plan, Source: source}, err
	}

	for range 2 {
		_, err := manager.CreateDebugRuntime(context.Background(), snapshot.ID, nil, RuntimeOptions{})
		if !errors.Is(err, ErrDebugSourceChanged) || !errors.Is(err, ErrCompilationFailed) {
			t.Fatalf("CreateDebugRuntime error = %v", err)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("debug compile calls = %d, want 1", calls.Load())
	}

	if closes.Load() != 1 {
		t.Fatalf("debug plan closes = %d, want 1", closes.Load())
	}
}

func TestSessionCloseWaitsForDebugRuntimeCompilationWithoutPublishingRuntime(t *testing.T) {
	var closes atomic.Int32
	manager, snapshot, engine := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	compileStarted := make(chan struct{})
	releaseCompile := make(chan struct{})
	parent.compileDebug = func(context.Context) (workspace.Compilation, error) {
		close(compileStarted)
		<-releaseCompile
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))

		return workspace.Compilation{Plan: plan, Source: parent.source}, err
	}

	type createResult struct {
		runtime *DebugRuntime
		err     error
	}
	createDone := make(chan createResult, 1)
	go func() {
		runtime, err := manager.CreateDebugRuntime(context.Background(), snapshot.ID, nil, RuntimeOptions{})
		createDone <- createResult{runtime: runtime, err: err}
	}()

	<-compileStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.CloseSession(ctx, snapshot.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseSession error = %v, want context.Canceled", err)
	}

	close(releaseCompile)
	result := <-createDone
	if result.runtime != nil || !errors.Is(result.err, ErrSessionClosed) {
		t.Fatalf("CreateDebugRuntime = (%v, %v), want nil ErrSessionClosed", result.runtime, result.err)
	}

	if err := manager.CloseSession(context.Background(), snapshot.ID); err != nil {
		t.Fatalf("wait for CloseSession: %v", err)
	}

	if closes.Load() != 2 {
		t.Fatalf("plan closes = %d, want normal and debug plans", closes.Load())
	}
}

func TestDebugRuntimeLeaseAndCloseHookPrecedePlanClosure(t *testing.T) {
	var closes atomic.Int32
	manager, snapshot, engine := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (workspace.Compilation, error) {
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))

		return workspace.Compilation{Plan: plan, Source: parent.source}, err
	}
	runtime, err := manager.CreateDebugRuntime(context.Background(), snapshot.ID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}
	if runtime.SessionID() != snapshot.ID || runtime.runtime.target.source != snapshot.Source ||
		runtime.runtime.target.text != "RETURN 1" {
		t.Fatalf("runtime identity = (%q, %+v, %q)", runtime.SessionID(),
			runtime.runtime.target.source, runtime.runtime.target.text)
	}

	hookCalled := make(chan struct{})
	manager.RegisterSessionCloseHook(func(_ context.Context, id SessionID) error {
		if id != snapshot.ID {
			t.Errorf("hook Session ID = %q, want %q", id, snapshot.ID)
		}

		close(hookCalled)

		return nil
	})

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- manager.CloseSession(context.Background(), snapshot.ID)
	}()

	<-hookCalled
	if closes.Load() != 0 {
		t.Fatalf("plans closed while runtime lease active: %d", closes.Load())
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("DebugRuntime.Close: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("repeated DebugRuntime.Close: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	if closes.Load() != 2 {
		t.Fatalf("plan closes = %d, want normal and debug plans", closes.Load())
	}
}

func TestDebugRuntimeSessionSetupFailureReleasesLease(t *testing.T) {
	manager, snapshot, engine := newHookedManager(t, "RETURN 1")
	parent := retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (workspace.Compilation, error) {
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))

		return workspace.Compilation{Plan: plan, Source: parent.source}, err
	}
	runtime, err := manager.CreateDebugRuntime(context.Background(), snapshot.ID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("DebugRuntime.Close: %v", err)
	}

	parent.mu.Lock()
	debugPlan := parent.debugPlan
	parent.mu.Unlock()
	if err := debugPlan.Close(); err != nil {
		t.Fatalf("debug Plan.Close: %v", err)
	}

	if _, err := manager.CreateDebugRuntime(
		context.Background(),
		snapshot.ID,
		nil,
		RuntimeOptions{},
	); err == nil {
		t.Fatal("CreateDebugRuntime succeeded with a closed debug Plan")
	}

	parent.mu.Lock()
	runtimes := parent.debugRuntimes
	parent.mu.Unlock()
	if runtimes != 0 {
		t.Fatalf("debug runtime leases = %d, want 0", runtimes)
	}

	if err := manager.CloseSession(context.Background(), snapshot.ID); err != nil {
		t.Fatalf("CloseSession after setup failure: %v", err)
	}
}

func TestDebugRuntimeCloseSharesFailureAndReleasesLeaseOnce(t *testing.T) {
	want := errors.New("debug runtime close failed")
	var calls atomic.Int32
	manager, snapshot, engine := newHookedManager(t, "RETURN 1", ferret.WithSessionCloseHook(func() error {
		calls.Add(1)

		return want
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (workspace.Compilation, error) {
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))

		return workspace.Compilation{Plan: plan, Source: parent.source}, err
	}
	runtime, err := manager.CreateDebugRuntime(context.Background(), snapshot.ID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateDebugRuntime: %v", err)
	}

	for range 2 {
		if err := runtime.Close(); !errors.Is(err, want) {
			t.Fatalf("DebugRuntime.Close error = %v, want %v", err, want)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("Ferret Session close calls = %d, want 1", calls.Load())
	}

	parent.mu.Lock()
	runtimes := parent.debugRuntimes
	parent.mu.Unlock()
	if runtimes != 0 {
		t.Fatalf("debug runtime leases = %d, want 0", runtimes)
	}
}
