package exec

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type closeOwnershipFixture struct {
	manager               *Manager
	session               *Session
	execution             *Execution
	sessionID             SessionID
	executionID           ExecutionID
	runStarted            chan struct{}
	runtimeCloseStarted   chan struct{}
	releaseRuntimeClose   chan struct{}
	planCloseStarted      chan struct{}
	releaseClose          func()
	runtimeCloseCalls     atomic.Int64
	planCloseCalls        atomic.Int64
	planClosedTooEarly    atomic.Bool
	runStartOnce          sync.Once
	runtimeCloseStartOnce sync.Once
	planCloseStartOnce    sync.Once
}

type observedDoneContext struct {
	context.Context

	once     sync.Once
	observed chan struct{}
}

func TestCloseExecutionRetainsSessionOwnershipUntilRuntimeCleanup(t *testing.T) {
	fixture := newCloseOwnershipFixture(t)
	executionClose := make(chan error, 1)
	go func() {
		executionClose <- fixture.manager.CloseExecution(context.Background(), fixture.executionID)
	}()

	waitForSignal(t, fixture.runtimeCloseStarted, "runtime Session close")
	assertExecutionOwnership(t, fixture.manager, fixture.execution, true)

	if _, err := fixture.manager.GetExecution(
		context.Background(),
		fixture.executionID,
	); !errors.Is(err, ErrExecutionNotFound) {
		t.Fatalf("GetExecution error = %v, want ErrExecutionNotFound", err)
	}

	sessionClose := make(chan error, 1)
	go func() {
		sessionClose <- fixture.manager.CloseSession(context.Background(), fixture.sessionID)
	}()

	waitForSessionClosing(t, fixture.session)
	assertExecutionOwnership(t, fixture.manager, fixture.execution, true)
	assertNoSignal(t, fixture.planCloseStarted, "Plan close before Execution cleanup")

	fixture.releaseClose()
	waitForResult(t, executionClose, "Execution close")
	waitForResult(t, sessionClose, "Session close")
	waitForSignal(t, fixture.planCloseStarted, "Plan close")
	assertExecutionOwnership(t, fixture.manager, fixture.execution, false)
	assertCloseCounts(t, fixture)
}

func TestCloseWorkspaceJoinsClosingSessionCleanup(t *testing.T) {
	want := errors.New("plan close failed")
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseClose) })
	}
	t.Cleanup(release)

	manager, session, _ := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		close(closeStarted)
		<-releaseClose

		return want
	}))

	sessionClose := make(chan error, 1)
	go func() {
		sessionClose <- manager.CloseSession(context.Background(), session.ID)
	}()
	waitForSignal(t, closeStarted, "Plan close")
	assertWorkspaceGroupOwnership(t, manager, session.Source.Workspace, session.ID, true)

	workspaceContext := newObservedDoneContext()
	workspaceClose := make(chan error, 1)
	go func() {
		workspaceClose <- manager.CloseWorkspace(workspaceContext, session.Source.Workspace)
	}()
	waitForSignal(t, workspaceContext.observed, "workspace close wait")

	release()
	waitForExpectedError(t, sessionClose, "Session close", want)
	waitForExpectedError(t, workspaceClose, "workspace close", want)
	assertWorkspaceGroupOwnership(t, manager, session.Source.Workspace, session.ID, false)
}

func TestManagerCloseJoinsCommittedWorkspaceClose(t *testing.T) {
	want := errors.New("plan close failed")
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseClose) })
	}
	t.Cleanup(release)

	manager, session, _ := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		close(closeStarted)
		<-releaseClose

		return want
	}))

	workspaceClose := make(chan error, 1)
	go func() {
		workspaceClose <- manager.CloseWorkspace(context.Background(), session.Source.Workspace)
	}()
	waitForSignal(t, closeStarted, "Plan close")

	managerContext := newObservedDoneContext()
	managerClose := make(chan error, 1)
	go func() {
		managerClose <- manager.Close(managerContext)
	}()
	waitForSignal(t, managerContext.observed, "Manager close wait")

	release()
	waitForExpectedError(t, workspaceClose, "workspace close", want)
	waitForExpectedError(t, managerClose, "Manager close", want)
	assertWorkspaceGroupOwnership(t, manager, session.Source.Workspace, session.ID, false)
}

func TestParentRetainedSessionPreservesCompletedCloseResult(t *testing.T) {
	want := errors.New("plan close failed")
	manager, snapshot, _ := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		return want
	}))

	retained := retainedSession(t, manager, snapshot.ID)

	if err := manager.CloseSession(context.Background(), snapshot.ID); !errors.Is(err, want) {
		t.Fatalf("CloseSession error = %v, want %v", err, want)
	}
	if err := manager.closeSession(context.Background(), snapshot.ID, retained); !errors.Is(err, want) {
		t.Fatalf("retained Session close error = %v, want %v", err, want)
	}
}

func TestCloseExecutionCallerCancellationRetainsSessionOwnership(t *testing.T) {
	fixture := newCloseOwnershipFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := fixture.manager.CloseExecution(ctx, fixture.executionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseExecution error = %v, want context.Canceled", err)
	}

	waitForSignal(t, fixture.runtimeCloseStarted, "runtime Session close")
	assertExecutionOwnership(t, fixture.manager, fixture.execution, true)

	sessionClose := make(chan error, 1)
	go func() {
		sessionClose <- fixture.manager.CloseSession(context.Background(), fixture.sessionID)
	}()

	waitForSessionClosing(t, fixture.session)
	assertExecutionOwnership(t, fixture.manager, fixture.execution, true)
	assertNoSignal(t, fixture.planCloseStarted, "Plan close before cancelled caller cleanup")

	fixture.releaseClose()
	waitForResult(t, sessionClose, "Session close")
	if err := fixture.manager.CloseExecution(context.Background(), fixture.executionID); err != nil {
		t.Fatalf("wait for committed Execution close: %v", err)
	}

	waitForSignal(t, fixture.planCloseStarted, "Plan close")
	assertExecutionOwnership(t, fixture.manager, fixture.execution, false)
	assertCloseCounts(t, fixture)
}

func TestConcurrentCloseExecutionUsesOneCleanupOwner(t *testing.T) {
	fixture := newCloseOwnershipFixture(t)
	const callers = 8

	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- fixture.manager.CloseExecution(context.Background(), fixture.executionID)
		}()
	}

	close(start)

	waitForSignal(t, fixture.runtimeCloseStarted, "runtime Session close")
	assertExecutionOwnership(t, fixture.manager, fixture.execution, true)

	sessionClose := make(chan error, 1)
	go func() {
		sessionClose <- fixture.manager.CloseSession(context.Background(), fixture.sessionID)
	}()
	waitForSessionClosing(t, fixture.session)

	fixture.releaseClose()
	for range callers {
		waitForResult(t, results, "concurrent Execution close")
	}

	waitForResult(t, sessionClose, "Session close")
	assertExecutionOwnership(t, fixture.manager, fixture.execution, false)
	assertCloseCounts(t, fixture)
}

func TestConcurrentCloseSessionSharesFailureAndOneOwner(t *testing.T) {
	const waiters = 8

	want := errors.New("plan close failed")
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	var closeCalls atomic.Int64
	var closeStartOnce sync.Once
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseClose) })
	}
	t.Cleanup(release)

	manager, session, _ := newHookedManager(t, "RETURN 1", ferret.WithPlanCloseHook(func() error {
		closeCalls.Add(1)
		closeStartOnce.Do(func() { close(closeStarted) })
		<-releaseClose

		return want
	}))

	results := make(chan error, waiters+1)
	go func() {
		results <- manager.CloseSession(context.Background(), session.ID)
	}()
	waitForSignal(t, closeStarted, "Plan close")

	for range waiters {
		ctx := newObservedDoneContext()
		go func() {
			results <- manager.CloseSession(ctx, session.ID)
		}()
		waitForSignal(t, ctx.observed, "concurrent close waiter")
	}

	release()
	for range waiters + 1 {
		if err := <-results; !errors.Is(err, want) {
			t.Fatalf("CloseSession error = %v, want %v", err, want)
		}
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("Plan close calls = %d, want 1", got)
	}
	if err := manager.CloseSession(context.Background(), session.ID); err != nil {
		t.Fatalf("late idempotent CloseSession: %v", err)
	}
}

func newCloseOwnershipFixture(t *testing.T) *closeOwnershipFixture {
	t.Helper()

	fixture := &closeOwnershipFixture{
		runStarted:          make(chan struct{}),
		runtimeCloseStarted: make(chan struct{}),
		releaseRuntimeClose: make(chan struct{}),
		planCloseStarted:    make(chan struct{}),
	}
	var releaseOnce sync.Once
	fixture.releaseClose = func() {
		releaseOnce.Do(func() { close(fixture.releaseRuntimeClose) })
	}

	manager, session, _ := newHookedManager(
		t,
		"RETURN WAITFOR FALSE TIMEOUT 30s EVERY 10ms",
		ferret.WithBeforeRunHook(func(ctx context.Context) (context.Context, error) {
			fixture.runStartOnce.Do(func() { close(fixture.runStarted) })

			return ctx, nil
		}),
		ferret.WithSessionCloseHook(func() error {
			fixture.runtimeCloseCalls.Add(1)
			fixture.runtimeCloseStartOnce.Do(func() { close(fixture.runtimeCloseStarted) })
			<-fixture.releaseRuntimeClose

			return nil
		}),
		ferret.WithPlanCloseHook(func() error {
			fixture.planCloseCalls.Add(1)
			if !fixture.execution.close.Finished() {
				fixture.planClosedTooEarly.Store(true)
			}

			fixture.planCloseStartOnce.Do(func() { close(fixture.planCloseStarted) })

			return nil
		}),
	)
	created, err := manager.CreateExecution(context.Background(), session.ID, nil, ExecutionOptions{})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	fixture.manager = manager
	fixture.session = retainedSession(t, manager, session.ID).session
	fixture.execution = retainedExecution(t, manager, created.ID).execution
	fixture.sessionID = session.ID
	fixture.executionID = created.ID
	t.Cleanup(fixture.releaseClose)

	if _, err := manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}

	waitForSignal(t, fixture.runStarted, "runtime Session run")

	return fixture
}

func newObservedDoneContext() *observedDoneContext {
	return &observedDoneContext{
		Context:  context.Background(),
		observed: make(chan struct{}),
	}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })

	return c.Context.Done()
}

func waitForSessionClosing(t *testing.T, session *Session) {
	t.Helper()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	for {
		if session.closeStarted() {
			return
		}

		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for Session close to start")
		default:
			runtime.Gosched()
		}
	}
}

func assertExecutionOwnership(t *testing.T, manager *Manager, execution *Execution, want bool) {
	t.Helper()

	manager.executions.mu.RLock()
	group := manager.executions.bySession[execution.session]
	var entry *executionEntry
	if group != nil {
		entry = group.entries[execution.id]
	}
	owned := entry != nil && entry.execution == execution
	manager.executions.mu.RUnlock()
	if owned != want {
		t.Fatalf("Session owns Execution = %t, want %t", owned, want)
	}
}

func assertWorkspaceGroupOwnership(
	t *testing.T,
	manager *Manager,
	workspaceID workspace.ID,
	sessionID SessionID,
	want bool,
) {
	t.Helper()

	manager.sessions.mu.RLock()
	group := manager.sessions.groups[workspaceID]
	var owned bool
	if group != nil {
		group.mu.Lock()
		owned = group.sessions[sessionID] != nil
		group.mu.Unlock()
	}
	manager.sessions.mu.RUnlock()
	if owned != want {
		t.Fatalf("workspace group owns Session = %t, want %t", owned, want)
	}
}

func assertCloseCounts(t *testing.T, fixture *closeOwnershipFixture) {
	t.Helper()

	if got := fixture.runtimeCloseCalls.Load(); got != 1 {
		t.Fatalf("runtime Session closes = %d, want 1", got)
	}

	if got := fixture.planCloseCalls.Load(); got != 1 {
		t.Fatalf("Plan closes = %d, want 1", got)
	}

	if fixture.planClosedTooEarly.Load() {
		t.Fatal("Plan.Close started before Execution close completed")
	}
}

func assertNoSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-signal:
		t.Fatal(description)
	default:
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForResult(t *testing.T, result <-chan error, description string) {
	t.Helper()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("%s: %v", description, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForExpectedError(t *testing.T, result <-chan error, description string, want error) {
	t.Helper()

	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("%s error = %v, want %v", description, err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
