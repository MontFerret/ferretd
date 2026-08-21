package debug

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/internal/exec"
)

func TestNewRequiresExecutionManager(t *testing.T) {
	manager, err := New(nil)
	if manager != nil {
		t.Fatal("New returned a manager for a nil execution dependency")
	}
	if !errors.Is(err, errNilExecutionManager) {
		t.Fatalf("New error = %v, want %v", err, errNilExecutionManager)
	}
}

func TestManagerDoesNotOwnExecutionManager(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	if fixture.manager.executions != fixture.executions {
		t.Fatal("New did not retain the supplied execution manager")
	}

	if err := fixture.manager.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := fixture.executions.GetSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("execution Session after debug manager Close: %v", err)
	}
	if _, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		nil,
		exec.RuntimeOptions{},
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("CreateSession after Close error = %v, want ErrClosed", err)
	}
}

func TestManagerCloseSettlesMultipleParentGroups(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	ctx := context.Background()
	secondParent, err := fixture.executions.CreateSession(ctx, fixture.workspace.ID(), "query.fql")
	if err != nil {
		t.Fatalf("CreateSession second parent: %v", err)
	}

	parents := []exec.SessionSnapshot{fixture.session, secondParent}
	created := make([]SessionSnapshot, 0, len(parents)*2)
	for _, parent := range parents {
		for range 2 {
			session, err := fixture.manager.CreateSession(ctx, parent.ID, nil, exec.RuntimeOptions{})
			if err != nil {
				t.Fatalf("CreateSession debug child: %v", err)
			}
			created = append(created, session)
		}
	}

	if err := fixture.manager.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, session := range created {
		if _, err := fixture.manager.GetSession(ctx, session.ID); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("GetSession after Close error = %v, want ErrSessionNotFound", err)
		}
	}
	for _, parent := range parents {
		if _, err := fixture.executions.GetSession(ctx, parent.ID); err != nil {
			t.Fatalf("borrowed execution Session after debug Close: %v", err)
		}
	}

	fixture.manager.mu.RLock()
	groups := len(fixture.manager.groups)
	fixture.manager.mu.RUnlock()
	if groups != 0 {
		t.Fatalf("debug parent groups after Close = %d, want 0", groups)
	}
	if err := fixture.manager.Close(ctx); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}

func TestCreateSessionCopiesParameters(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	input := map[string]any{
		"value":  7,
		"nested": map[string]any{"items": []any{"one", "two"}},
	}

	created, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		input,
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	input["value"] = 99
	input["nested"].(map[string]any)["items"].([]any)[0] = "caller"
	created.Parameters["nested"].(map[string]any)["items"].([]any)[1] = "snapshot"

	stored, err := fixture.manager.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Parameters["value"] != 7 ||
		stored.Parameters["nested"].(map[string]any)["items"].([]any)[0] != "one" ||
		stored.Parameters["nested"].(map[string]any)["items"].([]any)[1] != "two" {
		t.Fatalf("stored parameters = %#v, want immutable copy", stored.Parameters)
	}
}

func TestCreateSessionRejectsInvalidParameters(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")

	if _, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		map[string]any{"invalid": make(chan int)},
		exec.RuntimeOptions{},
	); !errors.Is(err, exec.ErrInvalidParameters) {
		t.Fatalf("CreateSession error = %v, want exec.ErrInvalidParameters", err)
	}
}

func TestDebugManagerRequiresContexts(t *testing.T) {
	t.Run("operation", func(t *testing.T) {
		fixture := newDebugFixture(t, "RETURN 1")
		assertPanics(t, func() {
			//lint:ignore SA1012 This test verifies that a required context cannot be nil.
			_, _ = fixture.manager.CreateSession(nil, fixture.session.ID, nil, exec.RuntimeOptions{})
		})
	})

	t.Run("close wait", func(t *testing.T) {
		fixture := newDebugFixture(t, "RETURN 1")
		created, err := fixture.manager.CreateSession(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		assertPanics(t, func() {
			//lint:ignore SA1012 This test verifies that a required context cannot be nil.
			_ = fixture.manager.CloseSession(nil, created.ID)
		})
	})
}

func TestDebugSessionLifecycleBreakpointsFramesScopesAndEvaluation(t *testing.T) {
	fixture := newDebugFixture(t, `LET box = {value: 10}
FUNC add(a) {
  LET b = a + 1
  RETURN b
}
LET result = add(@input)
RETURN {result, box}`)
	created, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		map[string]any{"input": 2},
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.State != StateCreated || created.Options.OutputContentType != "application/json" {
		t.Fatalf("created = %+v", created)
	}

	program := filepath.Join(fixture.workspace.Root(), "query.fql")
	breakpoints, err := fixture.manager.ReplaceBreakpoints(
		context.Background(),
		created.ID,
		program,
		[]BreakpointLocation{{Line: 2}},
	)
	if err != nil {
		t.Fatalf("ReplaceBreakpoints: %v", err)
	}
	if len(breakpoints) != 1 || !breakpoints[0].Verified || breakpoints[0].Line != 3 || breakpoints[0].ID == 0 {
		t.Fatalf("breakpoints = %+v", breakpoints)
	}

	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	running, err := fixture.manager.StartSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if running.State != StateRunning {
		t.Fatalf("start state = %v", running.State)
	}
	entry := waitForState(t, subscription, StateStopped)
	if entry.Reason != StopEntry {
		t.Fatalf("entry = %+v", entry)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	stopped := waitForState(t, subscription, StateStopped)
	if stopped.Reason != StopBreakpoint || stopped.Location.Line != 3 {
		t.Fatalf("breakpoint stop = %+v", stopped)
	}

	frames, err := fixture.manager.Frames(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	if len(frames) != 2 || frames[0].Index != 0 || frames[0].Name != "add" || frames[1].Index != 1 {
		t.Fatalf("frames = %+v", frames)
	}

	scopes, err := fixture.manager.Scopes(context.Background(), created.ID, 0)
	if err != nil {
		t.Fatalf("Scopes: %v", err)
	}
	if len(scopes) != 2 || scopes[0].Kind != ScopeLocals || scopes[1].Kind != ScopeParameters ||
		!debugScopeHas(scopes[0], "a", "2") || !debugScopeHas(scopes[1], "@input", "2") {
		t.Fatalf("scopes = %+v", scopes)
	}

	value, err := fixture.manager.Evaluate(context.Background(), created.ID, 1, "@input + 3")
	if err != nil || value.Display != "5" {
		t.Fatalf("caller evaluation = %+v, %v", value, err)
	}

	if _, err := fixture.manager.StepOverSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StepOverSession: %v", err)
	}
	stepped := waitForState(t, subscription, StateStopped)
	if stepped.Reason != StopStep || stepped.Location.Line != 4 {
		t.Fatalf("step stop = %+v", stepped)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("final ContinueSession: %v", err)
	}
	completed := waitForState(t, subscription, StateCompleted)
	if completed.Output == nil || completed.Output.ContentType != "application/json" {
		t.Fatalf("completed = %+v", completed)
	}
	if _, err := fixture.manager.Frames(context.Background(), created.ID); !errors.Is(err, ErrSessionNotStopped) {
		t.Fatalf("terminal Frames error = %v", err)
	}
}

func TestDebugSessionRuntimeErrorRemainsInspectableThenFailsOnResume(t *testing.T) {
	fixture := newDebugFixture(t, `LET x = 7
RETURN x / 0`)
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	runtimeError := waitForState(t, subscription, StateStopped)
	if runtimeError.Reason != StopRuntimeError || runtimeError.Failure == nil {
		t.Fatalf("runtime error = %+v", runtimeError)
	}
	if diagnostics := runtimeError.Failure.Diagnostics; len(diagnostics) != 1 ||
		diagnostics[0].URI != fixture.session.Source.URI || diagnostics[0].Code == "" ||
		diagnostics[0].Range.Start == diagnostics[0].Range.End {
		t.Fatalf("runtime error diagnostics = %+v, want one source-located diagnostic", diagnostics)
	}

	scopes, err := fixture.manager.Scopes(context.Background(), created.ID, 0)
	if err != nil || !debugScopeHas(scopes[0], "x", "7") {
		t.Fatalf("runtime-error scopes = %+v, %v", scopes, err)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("resume runtime error: %v", err)
	}
	failed := waitForState(t, subscription, StateFailed)
	if failed.Failure == nil || failed.Failure.Message == "" {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestDebugSessionTerminateCloseAndParentCascade(t *testing.T) {
	fixture := newDebugFixture(t, `RETURN FOR i IN 1..10000000
  RETURN i`)
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	if _, err := fixture.manager.TerminateSession(context.Background(), created.ID); err != nil {
		t.Fatalf("TerminateSession: %v", err)
	}
	terminated := waitForState(t, subscription, StateTerminated)
	if terminated.State != StateTerminated {
		t.Fatalf("terminated = %+v", terminated)
	}

	if err := fixture.executions.CloseSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
	if err := fixture.manager.CloseSession(context.Background(), created.ID); err != nil {
		t.Fatalf("idempotent CloseSession: %v", err)
	}
}

func TestDebugSessionCloseRacesTermination(t *testing.T) {
	fixture := newDebugFixture(t, `RETURN FOR i IN 1..10000000
  RETURN i`)
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}

	start := make(chan struct{})
	terminateDone := make(chan error, 1)
	closeDone := make(chan error, 1)
	go func() {
		<-start
		_, err := fixture.manager.TerminateSession(context.Background(), created.ID)
		terminateDone <- err
	}()
	go func() {
		<-start
		closeDone <- fixture.manager.CloseSession(context.Background(), created.ID)
	}()
	close(start)

	if err := <-terminateDone; err != nil && !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("TerminateSession: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
}

func TestSessionCloseCascadesRunningDebugSession(t *testing.T) {
	fixture := newDebugFixture(t, `RETURN FOR i IN 1..10000000
  RETURN i`)
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}

	if err := fixture.executions.CloseSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.executions.GetSession(context.Background(), fixture.session.ID); !errors.Is(err, exec.ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
}

func TestParentCloseWaitsForInFlightDebugCreation(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	parentID := fixture.session.ID
	if err := fixture.manager.beginCreate(parentID); err != nil {
		t.Fatalf("beginCreate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.manager.closeExecutionSession(ctx, parentID); !errors.Is(err, context.Canceled) {
		t.Fatalf("closeExecutionSession error = %v, want context.Canceled", err)
	}
	if err := fixture.manager.beginCreate(parentID); !errors.Is(err, exec.ErrSessionClosed) {
		t.Fatalf("beginCreate during parent close error = %v, want exec.ErrSessionClosed", err)
	}

	fixture.manager.mu.RLock()
	group := fixture.manager.groups[parentID]
	fixture.manager.mu.RUnlock()
	if err := group.gate.WaitClose(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent debug cleanup while creation was in flight = %v, want context.Canceled", err)
	}

	fixture.manager.finishCreate(parentID)
	if err := fixture.manager.closeExecutionSession(context.Background(), parentID); err != nil {
		t.Fatalf("wait for parent debug cleanup: %v", err)
	}

	fixture.manager.mu.RLock()
	_, retained := fixture.manager.groups[parentID]
	fixture.manager.mu.RUnlock()
	if retained {
		t.Fatal("parent debug group retained after cleanup")
	}
}

func TestDebugWatcherOverflowDisconnectsLaggingWatcher(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fixture.manager.mu.RLock()
	debugSession := fixture.manager.sessions[created.ID]
	fixture.manager.mu.RUnlock()
	lagging := debugSession.subscribe()
	independent := debugSession.subscribe()
	defer independent.Cancel()

	for range watcherBufferSize + 1 {
		debugSession.mu.Lock()
		debugSession.publishLocked(EventRunning, false)
		debugSession.mu.Unlock()
		<-independent.Events
	}

	var got error
	for watchErr := range lagging.Errors {
		got = watchErr
	}
	if !errors.Is(got, ErrWatcherLagged) {
		t.Fatalf("lag error = %v", got)
	}
}

func waitForState(t *testing.T, subscription Subscription, state State) SessionSnapshot {
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

func debugScopeHas(scope Scope, name, display string) bool {
	for _, variable := range scope.Variables {
		if variable.Name == name && variable.Value.Display == display {
			return true
		}
	}

	return false
}
