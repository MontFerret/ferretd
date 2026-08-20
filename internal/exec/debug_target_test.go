package exec

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/ferret/v2"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestAcquireDebugTargetCoordinatesCachesAndRetries(t *testing.T) {
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

				target, err := manager.AcquireDebugTarget(context.Background(), snapshot.ID)
				if target != nil {
					target.Release()
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
				t.Fatalf("AcquireDebugTarget: %v", err)
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
			_, err := manager.AcquireDebugTarget(ctx, snapshot.ID)
			result <- err
		}()

		<-started
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled acquire error = %v", err)
		}

		if _, err := manager.AcquireDebugTarget(context.Background(), snapshot.ID); !errors.Is(err, compileErr) {
			t.Fatalf("compile error = %v", err)
		}

		if _, err := manager.AcquireDebugTarget(context.Background(), snapshot.ID); !errors.Is(err, compileErr) {
			t.Fatalf("cached compile error = %v", err)
		}

		if calls.Load() != 2 {
			t.Fatalf("debug compile calls = %d, want 2", calls.Load())
		}
	})
}

func TestDebugTargetReleaseRequiresReceiver(t *testing.T) {
	var target *DebugTarget
	assertPanics(t, target.Release)
}

func TestAcquireDebugTargetRejectsChangedSourceAndCachesFailure(t *testing.T) {
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
		_, err := manager.AcquireDebugTarget(context.Background(), snapshot.ID)
		if !errors.Is(err, ErrDebugSourceChanged) || !errors.Is(err, ErrCompilationFailed) {
			t.Fatalf("AcquireDebugTarget error = %v", err)
		}
	}

	if calls.Load() != 1 {
		t.Fatalf("debug compile calls = %d, want 1", calls.Load())
	}

	if closes.Load() != 1 {
		t.Fatalf("debug plan closes = %d, want 1", closes.Load())
	}
}

func TestSessionCloseWaitsForDebugCompilationWithoutPublishingTarget(t *testing.T) {
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

	type acquireResult struct {
		target *DebugTarget
		err    error
	}
	acquireDone := make(chan acquireResult, 1)
	go func() {
		target, err := manager.AcquireDebugTarget(context.Background(), snapshot.ID)
		acquireDone <- acquireResult{target: target, err: err}
	}()

	<-compileStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.CloseSession(ctx, snapshot.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("CloseSession error = %v, want context.Canceled", err)
	}

	close(releaseCompile)
	result := <-acquireDone
	if result.target != nil || !errors.Is(result.err, ErrSessionClosed) {
		t.Fatalf("AcquireDebugTarget = (%v, %v), want nil ErrSessionClosed", result.target, result.err)
	}

	if err := manager.CloseSession(context.Background(), snapshot.ID); err != nil {
		t.Fatalf("wait for CloseSession: %v", err)
	}

	if closes.Load() != 2 {
		t.Fatalf("plan closes = %d, want normal and debug plans", closes.Load())
	}
}

func TestDebugTargetLeaseAndCloseHookPrecedePlanClosure(t *testing.T) {
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
	target, err := manager.AcquireDebugTarget(context.Background(), snapshot.ID)
	if err != nil {
		t.Fatalf("AcquireDebugTarget: %v", err)
	}
	if target.SessionID() != snapshot.ID || target.Source() != snapshot.Source || target.SourceText() != "RETURN 1" {
		t.Fatalf("target identity = (%q, %+v, %q)", target.SessionID(), target.Source(), target.SourceText())
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
		t.Fatalf("plans closed while target lease active: %d", closes.Load())
	}

	target.Release()
	target.Release()
	if err := <-closeDone; err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	if closes.Load() != 2 {
		t.Fatalf("plan closes = %d, want normal and debug plans", closes.Load())
	}
}
