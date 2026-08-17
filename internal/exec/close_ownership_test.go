package exec

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ferret "github.com/MontFerret/ferret/v2"
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

func TestCloseExecutionRetainsSessionOwnershipUntilRuntimeCleanup(t *testing.T) {
	fixture := newCloseOwnershipFixture(t)
	executionClose := make(chan error, 1)
	go func() {
		executionClose <- fixture.manager.CloseExecution(context.Background(), fixture.executionID)
	}()

	waitForSignal(t, fixture.runtimeCloseStarted, "runtime Session close")
	assertExecutionOwnership(t, fixture.session, fixture.execution, true)

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
	assertExecutionOwnership(t, fixture.session, fixture.execution, true)
	assertNoSignal(t, fixture.planCloseStarted, "Plan close before Execution cleanup")

	fixture.releaseClose()
	waitForResult(t, executionClose, "Execution close")
	waitForResult(t, sessionClose, "Session close")
	waitForSignal(t, fixture.planCloseStarted, "Plan close")
	assertExecutionOwnership(t, fixture.session, fixture.execution, false)
	assertCloseCounts(t, fixture)
}

func TestCloseExecutionCallerCancellationRetainsSessionOwnership(t *testing.T) {
	fixture := newCloseOwnershipFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := fixture.manager.CloseExecution(ctx, fixture.executionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseExecution error = %v, want context.Canceled", err)
	}

	waitForSignal(t, fixture.runtimeCloseStarted, "runtime Session close")
	assertExecutionOwnership(t, fixture.session, fixture.execution, true)

	sessionClose := make(chan error, 1)
	go func() {
		sessionClose <- fixture.manager.CloseSession(context.Background(), fixture.sessionID)
	}()

	waitForSessionClosing(t, fixture.session)
	assertExecutionOwnership(t, fixture.session, fixture.execution, true)
	assertNoSignal(t, fixture.planCloseStarted, "Plan close before cancelled caller cleanup")

	fixture.releaseClose()
	waitForResult(t, sessionClose, "Session close")
	if err := fixture.manager.CloseExecution(context.Background(), fixture.executionID); err != nil {
		t.Fatalf("wait for committed Execution close: %v", err)
	}

	waitForSignal(t, fixture.planCloseStarted, "Plan close")
	assertExecutionOwnership(t, fixture.session, fixture.execution, false)
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
	assertExecutionOwnership(t, fixture.session, fixture.execution, true)

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
	assertExecutionOwnership(t, fixture.session, fixture.execution, false)
	assertCloseCounts(t, fixture)
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
			select {
			case <-fixture.execution.closeDone:
			default:
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

	manager.mu.RLock()
	fixture.manager = manager
	fixture.session = manager.sessions[session.ID]
	fixture.execution = manager.executions[created.ID]
	manager.mu.RUnlock()
	fixture.sessionID = session.ID
	fixture.executionID = created.ID
	t.Cleanup(fixture.releaseClose)

	if _, err := manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}

	waitForSignal(t, fixture.runStarted, "runtime Session run")

	return fixture
}

func waitForSessionClosing(t *testing.T, session *Session) {
	t.Helper()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	for {
		session.mu.Lock()
		closing := session.closing
		session.mu.Unlock()
		if closing {
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

func assertExecutionOwnership(t *testing.T, session *Session, execution *Execution, want bool) {
	t.Helper()

	session.mu.Lock()
	owned := session.executions[execution.id] == execution
	session.mu.Unlock()
	if owned != want {
		t.Fatalf("Session owns Execution = %t, want %t", owned, want)
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
