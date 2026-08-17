package exec

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MontFerret/ferret/v2"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestDebugSessionLifecycleBreakpointsFramesScopesAndEvaluation(t *testing.T) {
	fixture := newExecutionFixture(t, `LET box = {value: 10}
FUNC add(a) {
  LET b = a + 1
  RETURN b
}
LET result = add(@input)
RETURN {result, box}`)
	created, err := fixture.manager.CreateDebugSession(
		context.Background(),
		fixture.session.ID,
		map[string]any{"input": 2},
		DebugSessionOptions{},
	)
	if err != nil {
		t.Fatalf("CreateDebugSession: %v", err)
	}
	if created.State != DebugStateCreated || created.Options.OutputContentType != "application/json" {
		t.Fatalf("created = %+v", created)
	}

	program := filepath.Join(fixture.workspace.Root(), "query.fql")
	breakpoints, err := fixture.manager.ReplaceDebugBreakpoints(
		context.Background(),
		created.ID,
		program,
		[]DebugBreakpointLocation{{Line: 2}},
	)
	if err != nil {
		t.Fatalf("ReplaceDebugBreakpoints: %v", err)
	}
	if len(breakpoints) != 1 || !breakpoints[0].Verified || breakpoints[0].Line != 3 || breakpoints[0].ID == 0 {
		t.Fatalf("breakpoints = %+v", breakpoints)
	}

	subscription, err := fixture.manager.WatchDebugSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchDebugSession: %v", err)
	}
	defer subscription.Cancel()

	running, err := fixture.manager.StartDebugSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("StartDebugSession: %v", err)
	}
	if running.State != DebugStateRunning {
		t.Fatalf("start state = %v", running.State)
	}
	entry := waitForDebugState(t, subscription, DebugStateStopped)
	if entry.Reason != DebugStopEntry {
		t.Fatalf("entry = %+v", entry)
	}

	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueDebugSession: %v", err)
	}
	stopped := waitForDebugState(t, subscription, DebugStateStopped)
	if stopped.Reason != DebugStopBreakpoint || stopped.Location.Line != 3 {
		t.Fatalf("breakpoint stop = %+v", stopped)
	}

	frames, err := fixture.manager.DebugFrames(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("DebugFrames: %v", err)
	}
	if len(frames) != 2 || frames[0].Index != 0 || frames[0].Name != "add" || frames[1].Index != 1 {
		t.Fatalf("frames = %+v", frames)
	}

	scopes, err := fixture.manager.DebugScopes(context.Background(), created.ID, 0)
	if err != nil {
		t.Fatalf("DebugScopes: %v", err)
	}
	if len(scopes) != 2 || scopes[0].Kind != DebugScopeLocals || scopes[1].Kind != DebugScopeParameters ||
		!debugScopeHas(scopes[0], "a", "2") || !debugScopeHas(scopes[1], "@input", "2") {
		t.Fatalf("scopes = %+v", scopes)
	}

	value, err := fixture.manager.EvaluateDebugSession(context.Background(), created.ID, 1, "@input + 3")
	if err != nil || value.Display != "5" {
		t.Fatalf("caller evaluation = %+v, %v", value, err)
	}

	if _, err := fixture.manager.StepOverDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StepOverDebugSession: %v", err)
	}
	stepped := waitForDebugState(t, subscription, DebugStateStopped)
	if stepped.Reason != DebugStopStep || stepped.Location.Line != 4 {
		t.Fatalf("step stop = %+v", stepped)
	}

	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("final ContinueDebugSession: %v", err)
	}
	completed := waitForDebugState(t, subscription, DebugStateCompleted)
	if completed.Output == nil || completed.Output.ContentType != "application/json" {
		t.Fatalf("completed = %+v", completed)
	}
	if _, err := fixture.manager.DebugFrames(context.Background(), created.ID); !errors.Is(err, ErrDebugSessionNotStopped) {
		t.Fatalf("terminal DebugFrames error = %v", err)
	}
}

func TestDebugSessionRuntimeErrorRemainsInspectableThenFailsOnResume(t *testing.T) {
	fixture := newExecutionFixture(t, `LET x = 7
RETURN x / 0`)
	created, err := fixture.manager.CreateDebugSession(context.Background(), fixture.session.ID, nil, DebugSessionOptions{})
	if err != nil {
		t.Fatalf("CreateDebugSession: %v", err)
	}
	subscription, err := fixture.manager.WatchDebugSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchDebugSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartDebugSession: %v", err)
	}
	waitForDebugState(t, subscription, DebugStateStopped)
	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueDebugSession: %v", err)
	}
	runtimeError := waitForDebugState(t, subscription, DebugStateStopped)
	if runtimeError.Reason != DebugStopRuntimeError || runtimeError.Failure == nil {
		t.Fatalf("runtime error = %+v", runtimeError)
	}

	scopes, err := fixture.manager.DebugScopes(context.Background(), created.ID, 0)
	if err != nil || !debugScopeHas(scopes[0], "x", "7") {
		t.Fatalf("runtime-error scopes = %+v, %v", scopes, err)
	}

	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("resume runtime error: %v", err)
	}
	failed := waitForDebugState(t, subscription, DebugStateFailed)
	if failed.Failure == nil || failed.Failure.Message == "" {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestDebugSessionTerminateCloseAndParentCascade(t *testing.T) {
	fixture := newExecutionFixture(t, `RETURN FOR i IN 1..10000000
  RETURN i`)
	created, err := fixture.manager.CreateDebugSession(context.Background(), fixture.session.ID, nil, DebugSessionOptions{})
	if err != nil {
		t.Fatalf("CreateDebugSession: %v", err)
	}
	subscription, err := fixture.manager.WatchDebugSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchDebugSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartDebugSession: %v", err)
	}
	waitForDebugState(t, subscription, DebugStateStopped)
	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueDebugSession: %v", err)
	}
	if _, err := fixture.manager.TerminateDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("TerminateDebugSession: %v", err)
	}
	terminated := waitForDebugState(t, subscription, DebugStateTerminated)
	if terminated.State != DebugStateTerminated {
		t.Fatalf("terminated = %+v", terminated)
	}

	if err := fixture.manager.CloseSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.manager.GetDebugSession(context.Background(), created.ID); !errors.Is(err, ErrDebugSessionNotFound) {
		t.Fatalf("GetDebugSession error = %v", err)
	}
	if err := fixture.manager.CloseDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("idempotent CloseDebugSession: %v", err)
	}
}

func TestDebugSessionCloseRacesTermination(t *testing.T) {
	fixture := newExecutionFixture(t, `RETURN FOR i IN 1..10000000
  RETURN i`)
	created, err := fixture.manager.CreateDebugSession(context.Background(), fixture.session.ID, nil, DebugSessionOptions{})
	if err != nil {
		t.Fatalf("CreateDebugSession: %v", err)
	}
	subscription, err := fixture.manager.WatchDebugSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchDebugSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartDebugSession: %v", err)
	}
	waitForDebugState(t, subscription, DebugStateStopped)
	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueDebugSession: %v", err)
	}

	start := make(chan struct{})
	terminateDone := make(chan error, 1)
	closeDone := make(chan error, 1)
	go func() {
		<-start
		_, err := fixture.manager.TerminateDebugSession(context.Background(), created.ID)
		terminateDone <- err
	}()
	go func() {
		<-start
		closeDone <- fixture.manager.CloseDebugSession(context.Background(), created.ID)
	}()
	close(start)

	if err := <-terminateDone; err != nil && !errors.Is(err, ErrDebugSessionNotFound) {
		t.Fatalf("TerminateDebugSession: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("CloseDebugSession: %v", err)
	}
	if _, err := fixture.manager.GetDebugSession(context.Background(), created.ID); !errors.Is(err, ErrDebugSessionNotFound) {
		t.Fatalf("GetDebugSession error = %v", err)
	}
}

func TestSessionCloseCascadesRunningDebugSession(t *testing.T) {
	fixture := newExecutionFixture(t, `RETURN FOR i IN 1..10000000
  RETURN i`)
	created, err := fixture.manager.CreateDebugSession(context.Background(), fixture.session.ID, nil, DebugSessionOptions{})
	if err != nil {
		t.Fatalf("CreateDebugSession: %v", err)
	}
	subscription, err := fixture.manager.WatchDebugSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchDebugSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartDebugSession: %v", err)
	}
	waitForDebugState(t, subscription, DebugStateStopped)
	if _, err := fixture.manager.ContinueDebugSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueDebugSession: %v", err)
	}

	if err := fixture.manager.CloseSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), fixture.session.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
	if _, err := fixture.manager.GetDebugSession(context.Background(), created.ID); !errors.Is(err, ErrDebugSessionNotFound) {
		t.Fatalf("GetDebugSession error = %v", err)
	}
}

func TestDebugCompilationCoordinatesCachesAndRetries(t *testing.T) {
	t.Run("concurrent_success", func(t *testing.T) {
		manager, snapshot, engine := newHookedManager(t, "RETURN 1")
		parent := manager.sessions[snapshot.ID]
		var calls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		parent.compileDebug = func(context.Context, string) (workspace.Compilation, error) {
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
				_, err := manager.CreateDebugSession(context.Background(), snapshot.ID, nil, DebugSessionOptions{})
				errorsChannel <- err
			}()
		}
		<-started
		close(release)
		wait.Wait()
		close(errorsChannel)
		for err := range errorsChannel {
			if err != nil {
				t.Fatalf("CreateDebugSession: %v", err)
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("debug compile calls = %d, want 1", calls.Load())
		}
	})

	t.Run("failure_cache_and_cancellation_retry", func(t *testing.T) {
		manager, snapshot, _ := newHookedManager(t, "RETURN 1")
		parent := manager.sessions[snapshot.ID]
		compileErr := errors.New("deterministic compile failure")
		var calls atomic.Int32
		started := make(chan struct{})
		parent.compileDebug = func(ctx context.Context, _ string) (workspace.Compilation, error) {
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
			_, err := manager.CreateDebugSession(ctx, snapshot.ID, nil, DebugSessionOptions{})
			result <- err
		}()
		<-started
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled create error = %v", err)
		}
		if _, err := manager.CreateDebugSession(context.Background(), snapshot.ID, nil, DebugSessionOptions{}); !errors.Is(err, compileErr) {
			t.Fatalf("compile error = %v", err)
		}
		if _, err := manager.CreateDebugSession(context.Background(), snapshot.ID, nil, DebugSessionOptions{}); !errors.Is(err, compileErr) {
			t.Fatalf("cached compile error = %v", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("debug compile calls = %d, want 2", calls.Load())
		}
	})
}

func TestDebugCompilationRejectsChangedSourceAndCachesFailure(t *testing.T) {
	var closes atomic.Int32
	manager, snapshot, engine := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := manager.sessions[snapshot.ID]
	var calls atomic.Int32
	parent.compileDebug = func(context.Context, string) (workspace.Compilation, error) {
		calls.Add(1)
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))
		source := parent.source
		source.Revision++

		return workspace.Compilation{Plan: plan, Source: source}, err
	}

	for range 2 {
		_, err := manager.CreateDebugSession(context.Background(), snapshot.ID, nil, DebugSessionOptions{})
		if !errors.Is(err, ErrDebugSourceChanged) || !errors.Is(err, ErrCompilationFailed) {
			t.Fatalf("CreateDebugSession error = %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("debug compile calls = %d, want 1", calls.Load())
	}
	if closes.Load() != 1 {
		t.Fatalf("debug plan closes = %d, want 1", closes.Load())
	}
}

func TestSessionCloseWaitsForDebugCompilationWithoutPublishing(t *testing.T) {
	var closes atomic.Int32
	manager, snapshot, engine := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closes.Add(1)

		return nil
	}))
	parent := manager.sessions[snapshot.ID]
	compileStarted := make(chan struct{})
	releaseCompile := make(chan struct{})
	parent.compileDebug = func(context.Context, string) (workspace.Compilation, error) {
		close(compileStarted)
		<-releaseCompile
		plan, err := engine.CompileDebug(context.Background(), ferretsource.New("query.fql", "RETURN 1"))

		return workspace.Compilation{Plan: plan, Source: parent.source}, err
	}

	createDone := make(chan error, 1)
	go func() {
		_, err := manager.CreateDebugSession(context.Background(), snapshot.ID, nil, DebugSessionOptions{})
		createDone <- err
	}()
	<-compileStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.CloseSession(ctx, snapshot.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseSession error = %v, want context.Canceled", err)
	}
	close(releaseCompile)
	if err := <-createDone; !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("CreateDebugSession error = %v, want ErrSessionClosed", err)
	}
	if err := manager.CloseSession(context.Background(), snapshot.ID); err != nil {
		t.Fatalf("wait for CloseSession: %v", err)
	}
	if closes.Load() != 2 {
		t.Fatalf("plan closes = %d, want normal and debug plans", closes.Load())
	}
	manager.mu.RLock()
	debugCount := len(manager.debugSessions) + len(manager.closingDebugs)
	manager.mu.RUnlock()
	if debugCount != 0 {
		t.Fatalf("published debug sessions = %d, want 0", debugCount)
	}
}

func TestDebugWatcherOverflowDisconnectsLaggingWatcher(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateDebugSession(context.Background(), fixture.session.ID, nil, DebugSessionOptions{})
	if err != nil {
		t.Fatalf("CreateDebugSession: %v", err)
	}
	fixture.manager.mu.RLock()
	debugSession := fixture.manager.debugSessions[created.ID]
	fixture.manager.mu.RUnlock()
	lagging := debugSession.Subscribe()
	independent := debugSession.Subscribe()
	defer independent.Cancel()

	for range watcherBufferSize + 1 {
		debugSession.mu.Lock()
		debugSession.publishLocked(DebugEventRunning, false)
		debugSession.mu.Unlock()
		<-independent.Events
	}

	var got error
	for watchErr := range lagging.Errors {
		got = watchErr
	}
	if !errors.Is(got, ErrDebugWatcherLagged) {
		t.Fatalf("lag error = %v", got)
	}
}

func waitForDebugState(t *testing.T, subscription DebugSubscription, state DebugState) DebugSessionSnapshot {
	t.Helper()

	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatalf("debug events closed before state %d", state)
			}
			if event.Snapshot.State == state {
				return event.Snapshot
			}
		case err := <-subscription.Errors:
			if err != nil {
				t.Fatalf("debug watch error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for debug state %d", state)
		}
	}
}

func debugScopeHas(scope DebugScope, name, display string) bool {
	for _, variable := range scope.Variables {
		if variable.Name == name && variable.Value.Display == display {
			return true
		}
	}

	return false
}
