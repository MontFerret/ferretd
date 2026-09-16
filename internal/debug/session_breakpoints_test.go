package debug

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestReplaceBreakpointsDelegatesAtomicRequest(t *testing.T) {
	for _, state := range []State{StateCreated, StateStopped, StateRunning} {
		t.Run(fmt.Sprint(state), func(t *testing.T) {
			fixture := newDebugFixture(t, "RETURN 1")

			created, err := fixture.manager.CreateSession(t.Context(), fixture.session.ID, nil, exec.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			debugger := fixture.runtime.latestDebugger()

			subscription, err := fixture.manager.WatchSession(t.Context(), created.ID)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(subscription.Cancel)

			if state != StateCreated {
				blockDebuggerOnContinue(t, debugger)

				if _, err := fixture.manager.StartSession(t.Context(), created.ID); err != nil {
					t.Fatal(err)
				}

				waitForState(t, subscription, StateStopped)

				if state == StateRunning {
					if _, err := fixture.manager.ContinueSession(t.Context(), created.ID); err != nil {
						t.Fatal(err)
					}

					waitForState(t, subscription, StateRunning)
				}
			}

			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			want := []apidebugger.Breakpoint{{ID: 17, Bound: true}, {ID: 18}}
			calls := 0
			var replacementErr error
			debugger.replaceFn = func(gotCtx context.Context, sourceName string, requests []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
				calls++

				if gotCtx != ctx || sourceName != "program" || len(requests) != 2 ||
					requests[0].Position != (apisource.Position{Line: 2}) ||
					requests[1].Position != (apisource.Position{Line: 2}) ||
					requests[0].Options.BindingMode != apidebugger.BreakpointBindNextExecutableInSource {
					t.Fatalf("replacement context/source/requests = %v, %q, %+v", gotCtx, sourceName, requests)
				}

				return want, replacementErr
			}

			locations := []apisource.Position{{Line: 2}, {Line: 2}}

			got, err := fixture.manager.ReplaceBreakpoints(ctx, created.ID, "program", locations)
			if err != nil || !reflect.DeepEqual(got, want) || calls != 1 {
				t.Fatalf("replacement = %+v, %v, calls %d", got, err, calls)
			}

			replacementErr = errors.New("atomic replacement rejected")
			if _, err := fixture.manager.ReplaceBreakpoints(ctx, created.ID, "program", locations); !errors.Is(err, replacementErr) {
				t.Fatalf("replacement failure = %v", err)
			}

			cancel()

			if _, err := fixture.manager.ReplaceBreakpoints(ctx, created.ID, "program", locations); !errors.Is(err, context.Canceled) || calls != 2 {
				t.Fatalf("canceled replacement = %v, calls %d", err, calls)
			}

			for _, command := range debugger.recordedCommands() {
				if command.name == "set breakpoint" || command.name == "delete breakpoint" || command.name == "pause" {
					t.Fatalf("unexpected command during replacement: %+v", command)
				}
			}
		})
	}
}

func TestReplaceBreakpointsRetainsPublishedSuccess(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")

	created, err := fixture.manager.CreateSession(t.Context(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	debugger := fixture.runtime.latestDebugger()
	debugger.replaceFn = func(context.Context, string, []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
		cancel()

		return []apidebugger.Breakpoint{{ID: 19}}, nil
	}

	got, err := fixture.manager.ReplaceBreakpoints(ctx, created.ID, "program", nil)
	if err != nil || len(got) != 1 || got[0].ID != 19 {
		t.Fatalf("published replacement = %+v, %v", got, err)
	}

	if _, err := fixture.manager.TerminateSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.manager.ReplaceBreakpoints(t.Context(), created.ID, "program", nil); !errors.Is(err, ErrSessionTerminal) {
		t.Fatalf("replacement after termination = %v", err)
	}
}

func TestDebugCommandRetainsOutputWithFailure(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")

	created, err := fixture.manager.CreateSession(t.Context(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("42")
	failure := errors.New("completion cleanup")
	debugger := fixture.runtime.latestDebugger()
	debugger.startFn = func(context.Context) (*apidebugger.Event, error) {
		return &apidebugger.Event{Reason: apidebugger.ReasonCompleted, Output: &api.Output{Content: content}}, failure
	}
	debugger.closeFn = func() error {
		content[0] = 'x'

		return nil
	}

	subscription, err := fixture.manager.WatchSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)

	if _, err := fixture.manager.StartSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	failed := waitForState(t, subscription, StateFailed)
	if failed.Output == nil || string(failed.Output.Content) != "42" || failed.Failure == nil || failed.Failure.Message != failure.Error() {
		t.Fatalf("failed snapshot = %+v", failed)
	}

	failed.Output.Content[0] = 'y'

	if err := debugger.Close(); err != nil {
		t.Fatal(err)
	}

	retained, err := fixture.manager.GetSession(t.Context(), created.ID)
	if err != nil || retained.Output == nil || string(retained.Output.Content) != "42" {
		t.Fatalf("retained snapshot = %+v, %v", retained, err)
	}
}

func TestReplaceBreakpointsPublicationWinsConcurrentCompletion(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")

	created, err := fixture.manager.CreateSession(t.Context(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	debugger := fixture.runtime.latestDebugger()
	release := make(chan struct{})
	debugger.startFn = func(ctx context.Context) (*apidebugger.Event, error) {
		select {
		case <-release:
			return &apidebugger.Event{Reason: apidebugger.ReasonCompleted}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	subscription, err := fixture.manager.WatchSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)

	if _, err := fixture.manager.StartSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	waitForState(t, subscription, StateRunning)
	debugger.replaceFn = func(context.Context, string, []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
		close(release)
		waitForState(t, subscription, StateCompleted)

		return []apidebugger.Breakpoint{{ID: 23}}, nil
	}

	bound, err := fixture.manager.ReplaceBreakpoints(t.Context(), created.ID, "program", nil)
	if err != nil || len(bound) != 1 || bound[0].ID != 23 {
		t.Fatalf("published replacement = %+v, %v", bound, err)
	}

	if _, err := fixture.manager.ReplaceBreakpoints(t.Context(), created.ID, "program", nil); !errors.Is(err, ErrSessionTerminal) {
		t.Fatalf("replacement after completion = %v", err)
	}
}

func TestPauseAndVariablesReceiveRequestContext(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")

	created, err := fixture.manager.CreateSession(t.Context(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	debugger := fixture.runtime.latestDebugger()
	blockDebuggerOnContinue(t, debugger)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	subscription, err := fixture.manager.WatchSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)

	if _, err := fixture.manager.StartSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	waitForState(t, subscription, StateStopped)

	if _, err := fixture.manager.Variables(ctx, created.ID, 1); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.manager.ContinueSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	waitForState(t, subscription, StateRunning)

	if _, err := fixture.manager.PauseSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	for _, command := range debugger.recordedCommands() {
		if (command.name == "pause" || command.name == "variables") && command.ctx != ctx {
			t.Fatalf("%s context = %v, want request context", command.name, command.ctx)
		}
	}

	cancel()

	if _, err := fixture.manager.PauseSession(ctx, created.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pause = %v", err)
	}
}
