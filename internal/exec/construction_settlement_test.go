package exec

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestConstructionPreservesBothCancellationOrders(t *testing.T) {
	for _, phase := range []string{"compile", "parameters", "debug compile", "debug session"} {
		parents := []string{"workspace", "manager"}

		if phase == "debug compile" || phase == "debug session" {
			parents = []string{"session"}
		}

		for _, parent := range parents {
			for _, order := range []string{"caller first", "parent first"} {
				t.Run(phase+"/"+parent+"/"+order, func(t *testing.T) {
					fixture := newExecutionFixture(t, "RETURN 1")
					started, canceled, resume := make(chan struct{}), make(chan struct{}), make(chan struct{})
					release := sync.OnceFunc(func() { close(resume) })
					t.Cleanup(release)
					block := func(ctx context.Context) {
						close(started)
						<-ctx.Done()
						close(canceled)
						<-resume
					}

					create := func(ctx context.Context) error {
						created, err := fixture.manager.CreateSession(ctx, fixture.workspace.ID(), "query.fql", "")
						if created.ID != "" {
							t.Error("published a canceled Session")
						}

						return err
					}
					closedCause := workspace.ErrClosed

					switch phase {
					case "compile":
						fixture.runtime.compileFn = func(ctx context.Context) (api.Plan, error) {
							block(ctx)

							return &planSpy{runtime: fixture.runtime}, ctx.Err()
						}
					case "parameters":
						fixture.runtime.paramsFn = func(ctx context.Context) ([]string, error) {
							block(ctx)

							return nil, ctx.Err()
						}
					case "debug compile", "debug session":
						closedCause = ErrSessionClosed
						owner := retainedSession(t, fixture.manager, fixture.session.ID).session
						owner.compileDebug = func(ctx context.Context) (api.Plan, error) {
							if phase == "debug compile" {
								block(ctx)

								return &planSpy{runtime: fixture.runtime}, ctx.Err()
							}

							return &planSpy{runtime: fixture.runtime, newDebugFn: func(ctx context.Context, _ ...api.SessionOption) (apidebugger.Session, error) {
								block(ctx)

								return nil, ctx.Err()
							}}, nil
						}
						create = func(ctx context.Context) error {
							created, err := fixture.manager.CreateDebugRuntime(ctx, fixture.session.ID, nil, RuntimeOptions{})
							if created != nil {
								t.Error("published a canceled debugger")
								_ = created.Close()
							}

							return err
						}
					}

					closeParent := fixture.manager.Close
					switch parent {
					case "workspace":
						closeParent = func(ctx context.Context) error { return fixture.manager.CloseWorkspace(ctx, fixture.workspace.ID()) }
					case "session":
						closeParent = func(ctx context.Context) error { return fixture.manager.CloseSession(ctx, fixture.session.ID) }
					}

					ctx, cancel := context.WithCancelCause(t.Context())
					defer cancel(nil)
					callerCause := errors.New("caller stopped construction")
					result := make(chan error, 1)
					go func() { result <- create(ctx) }()
					waitForSignal(t, started, "preparation")

					if order == "caller first" {
						cancel(callerCause)
						waitForSignal(t, canceled, "caller cancellation")
					}

					waitCtx, stop := context.WithCancel(t.Context())
					stop()

					if err := closeParent(waitCtx); !errors.Is(err, context.Canceled) {
						t.Fatalf("canceled close waiter = %v", err)
					}

					if order == "parent first" {
						waitForSignal(t, canceled, "parent cancellation")
						cancel(callerCause)
					}

					release()

					err := receiveConstructionError(t, result, "canceled construction")
					if !errors.Is(err, context.Canceled) || !errors.Is(err, callerCause) || !errors.Is(err, closedCause) {
						t.Fatalf("construction error = %v", err)
					}

					if err := closeParent(t.Context()); err != nil {
						t.Fatalf("parent retained cancellation as cleanup failure: %v", err)
					}
				})
			}
		}
	}
}

func TestPendingPreparationErrorsDoNotBecomeCloseFailures(t *testing.T) {
	for _, phase := range []string{"compile", "parameters", "debug compile", "debug session"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newExecutionFixture(t, "RETURN 1")
			parent := retainedSession(t, fixture.manager, fixture.session.ID).session
			primaryErr := errors.New("preparation failed")
			started := make(chan struct{})
			failure := func(ctx context.Context) error {
				close(started)
				<-ctx.Done()

				return errors.Join(primaryErr, ctx.Err())
			}
			create := func() error {
				_, err := fixture.manager.CreateSession(t.Context(), fixture.workspace.ID(), "query.fql", "")

				return err
			}
			closeParent := func() error { return fixture.manager.CloseWorkspace(t.Context(), fixture.workspace.ID()) }

			switch phase {
			case "compile":
				fixture.runtime.compileFn = func(ctx context.Context) (api.Plan, error) { return nil, failure(ctx) }
			case "parameters":
				fixture.runtime.paramsFn = func(ctx context.Context) ([]string, error) { return nil, failure(ctx) }
			case "debug compile", "debug session":
				create = func() error {
					_, err := fixture.manager.CreateDebugRuntime(t.Context(), fixture.session.ID, nil, RuntimeOptions{})

					return err
				}
				closeParent = func() error { return fixture.manager.CloseSession(t.Context(), fixture.session.ID) }
				parent.compileDebug = func(ctx context.Context) (api.Plan, error) {
					if phase == "debug compile" {
						return nil, failure(ctx)
					}

					return &planSpy{runtime: fixture.runtime, newDebugFn: func(ctx context.Context, _ ...api.SessionOption) (apidebugger.Session, error) {
						return nil, failure(ctx)
					}}, nil
				}
			}

			result := make(chan error, 1)
			go func() { result <- create() }()
			waitForSignal(t, started, "preparation")
			closed := make(chan error, 1)
			go func() { closed <- closeParent() }()

			if err := receiveConstructionError(t, closed, "parent close"); err != nil {
				t.Fatalf("parent retained preparation error: %v", err)
			}

			if err := receiveConstructionError(t, result, "preparation failure"); !errors.Is(err, primaryErr) || !errors.Is(err, context.Canceled) {
				t.Fatalf("creation = %v", err)
			}
		})
	}
}

func TestRollbackSettledBeforeCloseStaysWithCreator(t *testing.T) {
	for _, phase := range []string{"compile", "debug compile", "debug session"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newExecutionFixture(t, "RETURN 1")
			parent := retainedSession(t, fixture.manager, fixture.session.ID).session
			primaryErr, cleanupErr := errors.New("preparation failed"), errors.New("rollback failed")
			plan := &planSpy{runtime: fixture.runtime, closeFn: func() error { return cleanupErr }}
			var err error

			if phase == "compile" {
				fixture.runtime.compileFn = func(context.Context) (api.Plan, error) { return plan, primaryErr }
				_, err = fixture.manager.CreateSession(t.Context(), fixture.workspace.ID(), "query.fql", "")
			} else {
				parent.compileDebug = func(context.Context) (api.Plan, error) {
					if phase == "debug compile" {
						return plan, primaryErr
					}

					return &planSpy{runtime: fixture.runtime, newDebugFn: func(context.Context, ...api.SessionOption) (apidebugger.Session, error) {
						return &debugSessionSpy{runtime: fixture.runtime}, primaryErr
					}}, nil
				}
				fixture.runtime.sessionClose = func() error { return cleanupErr }
				_, err = fixture.manager.CreateDebugRuntime(t.Context(), fixture.session.ID, nil, RuntimeOptions{})
			}

			if !errors.Is(err, primaryErr) || !errors.Is(err, cleanupErr) {
				t.Fatalf("creation = %v", err)
			}

			if err := fixture.manager.Close(t.Context()); err != nil {
				t.Fatalf("parent retained completed rollback: %v", err)
			}
		})
	}
}

func TestCanceledCloseWaiterPreservesPendingRollbackFailure(t *testing.T) {
	for _, parentKind := range []string{"workspace", "session"} {
		t.Run(parentKind, func(t *testing.T) {
			fixture := newExecutionFixture(t, "RETURN 1")
			parent := retainedSession(t, fixture.manager, fixture.session.ID).session
			started, closing, resume := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			t.Cleanup(release)
			cleanupErr := errors.New("pending rollback failed")
			closePlan := func() error {
				close(closing)
				<-resume

				return cleanupErr
			}
			compile := func(ctx context.Context) (api.Plan, error) {
				close(started)
				<-ctx.Done()

				return &planSpy{runtime: fixture.runtime, closeFn: closePlan}, ctx.Err()
			}
			create := func() error {
				_, err := fixture.manager.CreateSession(t.Context(), fixture.workspace.ID(), "query.fql", "")

				return err
			}
			closeParent := func(ctx context.Context) error { return fixture.manager.CloseWorkspace(ctx, fixture.workspace.ID()) }

			if parentKind == "workspace" {
				fixture.runtime.compileFn = compile
			} else {
				parent.compileDebug = compile
				create = func() error {
					_, err := fixture.manager.CreateDebugRuntime(t.Context(), fixture.session.ID, nil, RuntimeOptions{})

					return err
				}
				closeParent = func(ctx context.Context) error { return fixture.manager.CloseSession(ctx, fixture.session.ID) }
			}

			result := make(chan error, 1)
			go func() { result <- create() }()
			waitForSignal(t, started, "preparation")
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			if err := closeParent(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled close wait = %v", err)
			}

			waitForSignal(t, closing, "rollback")
			waitCtx := newObservedDoneContext()
			closed := make(chan error, 1)
			go func() { closed <- closeParent(waitCtx) }()
			waitForSignal(t, waitCtx.observed, "retained close waiter")
			release()

			if err := receiveConstructionError(t, result, "rollback"); !errors.Is(err, cleanupErr) {
				t.Fatalf("creation = %v", err)
			}

			if err := receiveConstructionError(t, closed, "committed close"); !errors.Is(err, cleanupErr) {
				t.Fatalf("close lost rollback failure: %v", err)
			}
		})
	}
}
