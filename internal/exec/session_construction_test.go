package exec

import (
	"context"
	"crypto/rand"
	"errors"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestParentCloseCancelsSessionPreparation(t *testing.T) {
	for _, phase := range []string{"compile", "parameters"} {
		for _, parent := range []string{"workspace", "manager"} {
			t.Run(phase+"/"+parent, func(t *testing.T) {
				fixture := newExecutionFixture(t, "RETURN 1")
				started := make(chan struct{})
				cleanupErr := errors.New("unpublished plan cleanup")
				var closes atomic.Int32
				plan := &planSpy{runtime: fixture.runtime, closeFn: func() error { closes.Add(1); return cleanupErr }}

				if phase == "compile" {
					fixture.runtime.compileFn = func(ctx context.Context) (api.Plan, error) {
						close(started)
						<-ctx.Done()

						return plan, ctx.Err()
					}
				} else {
					fixture.runtime.compileFn = func(context.Context) (api.Plan, error) { return plan, nil }
					fixture.runtime.paramsFn = func(ctx context.Context) ([]string, error) {
						close(started)
						<-ctx.Done()

						return nil, ctx.Err()
					}
				}

				result := make(chan error, 1)
				go func() {
					created, err := fixture.manager.CreateSession(t.Context(), fixture.workspace.ID(), "query.fql", "")
					if created.ID != "" {
						t.Error("published a cancelled Session")
					}

					result <- err
				}()
				waitForSignal(t, started, "preparation")
				closed := make(chan error, 1)
				go func() {
					if parent == "workspace" {
						closed <- fixture.workspaces.Close(t.Context(), fixture.workspace.ID())
					} else {
						closed <- fixture.manager.Close(t.Context())
					}
				}()

				err := receiveConstructionError(t, result, "cancelled preparation")
				if !errors.Is(err, context.Canceled) || !errors.Is(err, workspace.ErrClosed) || !errors.Is(err, cleanupErr) {
					t.Fatalf("preparation error = %v", err)
				}

				if err := receiveConstructionError(t, closed, "parent close"); !errors.Is(err, cleanupErr) {
					t.Fatalf("parent close = %v", err)
				}

				if closes.Load() != 1 {
					t.Fatalf("unpublished plan closes = %d, want 1", closes.Load())
				}
			})
		}
	}
}

func TestSessionCloseCancelsDebugPreparation(t *testing.T) {
	for _, phase := range []string{"compile", "new session"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newExecutionFixture(t, "RETURN 1")
			parent := retainedSession(t, fixture.manager, fixture.session.ID).session
			started := make(chan struct{})
			cleanupErr := errors.New("debug cleanup")
			var debugCloses atomic.Int32

			if phase == "compile" {
				parent.compileDebug = func(ctx context.Context) (api.Plan, error) {
					close(started)
					<-ctx.Done()

					return &planSpy{runtime: fixture.runtime, closeFn: func() error { debugCloses.Add(1); return cleanupErr }}, ctx.Err()
				}
			} else {
				parent.compileDebug = func(context.Context) (api.Plan, error) {
					return &planSpy{runtime: fixture.runtime, newDebugFn: func(ctx context.Context, _ ...api.SessionOption) (apidebugger.Session, error) {
						close(started)
						<-ctx.Done()

						return &debugSessionSpy{runtime: fixture.runtime}, nil
					}}, nil
				}
				fixture.runtime.sessionClose = func() error { debugCloses.Add(1); return cleanupErr }
			}

			result := make(chan error, 1)
			go func() {
				created, err := fixture.manager.CreateDebugRuntime(t.Context(), fixture.session.ID, nil, RuntimeOptions{})
				if created != nil {
					t.Error("published a cancelled debugger")
					_ = created.Close()
				}

				result <- err
			}()
			waitForSignal(t, started, "debug preparation")
			closed := make(chan error, 1)
			go func() { closed <- fixture.manager.CloseSession(t.Context(), fixture.session.ID) }()

			err := receiveConstructionError(t, result, "debug cancellation")
			if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrSessionClosed) {
				t.Fatalf("debug creation = %v", err)
			}

			if !errors.Is(err, cleanupErr) || debugCloses.Load() != 1 {
				t.Fatalf("debug rollback = %v, closes %d", err, debugCloses.Load())
			}

			if err := receiveConstructionError(t, closed, "Session close"); !errors.Is(err, cleanupErr) {
				t.Fatalf("Session close = %v, want cleanup failure", err)
			}
		})
	}
}

func TestSessionCreationRollsBackInvalidAndLatePlans(t *testing.T) {
	for _, phase := range []string{"nil plan", "id", "cancelled publication"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newExecutionFixture(t, "RETURN 1")
			cleanupErr := errors.New("plan cleanup")
			var closes atomic.Int32
			fixture.runtime.planClose = func() error { closes.Add(1); return cleanupErr }
			t.Cleanup(func() { fixture.runtime.planClose = nil })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var want error
			switch phase {
			case "nil plan":
				fixture.runtime.compileFn = func(context.Context) (api.Plan, error) { return nil, nil }
				want = ErrCompilationFailed
			case "id":
				want = errors.New("random source failed")
				uuid.SetRand(iotest.ErrReader(want))
				defer uuid.SetRand(rand.Reader)
			case "cancelled publication":
				want = context.Canceled
				fixture.runtime.paramsFn = func(context.Context) ([]string, error) { cancel(); return nil, nil }
			}

			created, err := fixture.manager.CreateSession(ctx, fixture.workspace.ID(), "query.fql", "")
			if created.ID != "" || !errors.Is(err, want) {
				t.Fatalf("CreateSession = %+v, %v", created, err)
			}

			if phase != "nil plan" && (!errors.Is(err, cleanupErr) || closes.Load() != 1) {
				t.Fatalf("rollback = %v, closes %d", err, closes.Load())
			}
		})
	}
}

func receiveConstructionError(t *testing.T, result <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)

		return nil
	}
}
