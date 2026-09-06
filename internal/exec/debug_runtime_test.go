package exec

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
)

func TestCreateDebugRuntimeCoordinatesCachesAndRetries(t *testing.T) {
	t.Run("concurrent_success", func(t *testing.T) {
		manager, snapshot, runtimeSpy := newHookedManager(t, "RETURN 1")
		parent := retainedSession(t, manager, snapshot.ID).session
		var calls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		parent.compileDebug = func(context.Context) (api.Plan, error) {
			if calls.Add(1) == 1 {
				close(started)
			}

			<-release

			return runtimeSpy.CompileDebug(context.Background(), api.NewSource("/query.fql", "RETURN 1"))
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
		parent.compileDebug = func(ctx context.Context) (api.Plan, error) {
			switch calls.Add(1) {
			case 1:
				close(started)
				<-ctx.Done()

				return nil, ctx.Err()
			default:
				return nil, compileErr
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

func TestDebugRuntimePreparesParametersOptionsAndCancellation(t *testing.T) {
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
		RuntimeOptions{
			OutputContentType: " \t\n",
			WorkingDirectory:  workingDirectory,
		},
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

	options := runtime.Options()
	if options.OutputContentType != defaultOutputContentType {
		t.Fatalf("OutputContentType = %q, want %q", options.OutputContentType, defaultOutputContentType)
	}

	canonicalWorkingDirectory, err := filepath.EvalSymlinks(workingDirectory)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	if options.WorkingDirectory != filepath.Clean(canonicalWorkingDirectory) {
		t.Fatalf(
			"working directory = %q, want %q",
			options.WorkingDirectory,
			canonicalWorkingDirectory,
		)
	}

	parent := retainedSession(t, fixture.manager, fixture.session.ID).session

	plan := parent.debugPlan.(*planSpy)
	if got := plan.lastOptions; got.params["value"] != 7 ||
		got.contentType != defaultOutputContentType || got.fsRoot != canonicalWorkingDirectory {
		t.Fatalf("debug session options = %+v", got)
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("DebugRuntime.Close: %v", err)
	}

	if !errors.Is(runtime.Context().Err(), context.Canceled) {
		t.Fatalf("runtime context error = %v, want context.Canceled", runtime.Context().Err())
	}
}

func TestSessionCloseWaitsForDebugRuntimeCompilationWithoutPublishingRuntime(t *testing.T) {
	var closes atomic.Int32
	manager, snapshot, runtimeSpy := newHookedManager(t, "RETURN 1", withPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	compileStarted := make(chan struct{})
	releaseCompile := make(chan struct{})
	parent.compileDebug = func(context.Context) (api.Plan, error) {
		close(compileStarted)
		<-releaseCompile

		return runtimeSpy.CompileDebug(context.Background(), api.NewSource("/query.fql", "RETURN 1"))
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
	manager, snapshot, runtimeSpy := newHookedManager(t, "RETURN 1", withPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (api.Plan, error) {
		return runtimeSpy.CompileDebug(context.Background(), api.NewSource("/query.fql", "RETURN 1"))
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
	manager, snapshot, runtimeSpy := newHookedManager(t, "RETURN 1")
	parent := retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (api.Plan, error) {
		return runtimeSpy.CompileDebug(context.Background(), api.NewSource("/query.fql", "RETURN 1"))
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

func TestDebugRuntimeClosesPartialSessionOnSetupFailure(t *testing.T) {
	want := errors.New("debug session creation failed")
	closeErr := errors.New("partial session cleanup failed")
	var closes atomic.Int32
	var parent *session
	manager, snapshot, runtime := newHookedManager(
		t,
		"RETURN 1",
		withSessionCloseHook(func() error {
			closes.Add(1)
			parent.mu.Lock()
			leases := parent.debugRuntimes
			parent.mu.Unlock()

			if leases != 1 {
				t.Errorf("leases during partial session cleanup = %d, want 1", leases)
			}

			return closeErr
		}),
	)
	parent = retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (api.Plan, error) {
		return &planSpy{
			runtime: runtime,
			newDebugFn: func(context.Context, ...api.SessionOption) (apidebugger.Session, error) {
				return &debugSessionSpy{runtime: runtime}, want
			},
		}, nil
	}

	created, err := manager.CreateDebugRuntime(context.Background(), snapshot.ID, nil, RuntimeOptions{})
	if created != nil || !errors.Is(err, want) || !errors.Is(err, closeErr) {
		t.Fatalf("CreateDebugRuntime = %v, %v; want nil, %v", created, err, want)
	}

	if closes.Load() != 1 {
		t.Fatalf("partial debugger Session close calls = %d, want 1", closes.Load())
	}

	parent.mu.Lock()
	runtimes := parent.debugRuntimes
	parent.mu.Unlock()

	if runtimes != 0 {
		t.Fatalf("debug runtime leases = %d, want 0", runtimes)
	}
}

func TestDebugRuntimeCloseSharesFailureAndReleasesLeaseOnce(t *testing.T) {
	want := errors.New("debug runtime close failed")
	var calls atomic.Int32
	manager, snapshot, runtimeSpy := newHookedManager(t, "RETURN 1", withSessionCloseHook(func() error {
		calls.Add(1)

		return want
	}))
	parent := retainedSession(t, manager, snapshot.ID).session
	parent.compileDebug = func(context.Context) (api.Plan, error) {
		return runtimeSpy.CompileDebug(context.Background(), api.NewSource("/query.fql", "RETURN 1"))
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
		t.Fatalf("runtime Session close calls = %d, want 1", calls.Load())
	}

	parent.mu.Lock()
	runtimes := parent.debugRuntimes
	parent.mu.Unlock()

	if runtimes != 0 {
		t.Fatalf("debug runtime leases = %d, want 0", runtimes)
	}
}
