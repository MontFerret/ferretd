package debug

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferretd/internal/exec"
)

// Inject a controllable logical connection into the debug child. The framed DAP
// test covers the real Wire binding; these gates order command and close races.
func connectionSession(t *testing.T) (*session, *debuggerSessionSpy, lostConnection, Subscription) {
	t.Helper()
	f := newDebugFixture(t, "RETURN 1")

	child, err := f.manager.CreateSession(t.Context(), f.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	s, err := f.manager.session(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}

	if s.connection != nil {
		t.Fatal("native runtime unexpectedly has a connection observer")
	}

	loss := make(lostConnection)
	s.runtimeName, s.connection = "App", loss

	return s, f.runtime.latestDebugger(), loss, s.subscribe()
}

func TestConnectionLossAtStop(t *testing.T) {
	for _, stage := range []string{"before first stop", "at breakpoint"} {
		t.Run(stage, func(t *testing.T) {
			s, spy, loss, watch := connectionSession(t)
			spy.startFn = func(context.Context) (*apidebugger.Event, error) {
				return &apidebugger.Event{Reason: apidebugger.ReasonBreakpoint}, nil
			}

			if stage == "before first stop" {
				close(loss)
			}

			if _, err := s.start(t.Context()); err != nil {
				t.Fatal(err)
			}

			expectConnectionEvent(t, watch, EventRunning)

			if stage == "at breakpoint" {
				expectConnectionEvent(t, watch, EventStopped)
				close(loss)
			}

			failed := expectConnectionEvent(t, watch, EventFailed)
			if failed.Failure == nil || !strings.Contains(failed.Failure.Message, `"App": application disconnected`) {
				t.Fatalf("failure = %+v", failed.Failure)
			}

			s.requestTermination()

			if s.snapshot().State != StateFailed {
				t.Fatal("Stop replaced a committed connection failure")
			}

			if _, ok := <-watch.Events; ok {
				t.Fatal("duplicate terminal event")
			}
		})
	}
}

// A synctest bubble must join all its goroutines. Leave the healthy connection
// open so only the terminal debug event can release the passive observer.
func TestConnectionObserverEndsAtHealthyCompletion(t *testing.T) {
	f := newDebugFixture(t, "RETURN 1")

	runtime, err := f.executions.CreateDebugRuntime(t.Context(), f.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})

	synctest.Test(t, func(t *testing.T) {
		s := newSession("test", runtime)
		s.connection = make(lostConnection)
		watch := s.subscribe()

		if _, err := s.start(t.Context()); err != nil {
			t.Fatal(err)
		}

		expectConnectionEvent(t, watch, EventRunning)
		expectConnectionEvent(t, watch, EventStopped)
		synctest.Wait()

		if _, err := s.continueExecution(t.Context()); err != nil {
			t.Fatal(err)
		}

		expectConnectionEvent(t, watch, EventRunning)
		expectConnectionEvent(t, watch, EventCompleted)
	})
}

func TestConnectionLossDuringCommand(t *testing.T) {
	for _, test := range []struct {
		name   string
		reason apidebugger.Reason
		err    error
		want   EventKind
	}{
		{"completion", apidebugger.ReasonCompleted, nil, EventCompleted},
		{"failure with output", apidebugger.ReasonCompleted, errors.New("command failed"), EventFailed},
		{"next suspension", apidebugger.ReasonStep, nil, EventFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, spy, loss, watch := connectionSession(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			spy.continueFn = func(context.Context) (*apidebugger.Event, error) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}

				return &apidebugger.Event{Reason: test.reason, Output: &api.Content{Data: []byte("42")}}, test.err
			}

			if _, err := s.start(ctx); err != nil {
				t.Fatal(err)
			}

			expectConnectionEvent(t, watch, EventRunning)
			expectConnectionEvent(t, watch, EventStopped)

			if _, err := s.continueExecution(ctx); err != nil {
				t.Fatal(err)
			}

			expectConnectionEvent(t, watch, EventRunning)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			close(loss)
			close(release)

			final := expectConnectionEvent(t, watch, test.want)
			if test.reason == apidebugger.ReasonCompleted && (final.Output == nil || string(final.Output.Data) != "42") {
				t.Fatalf("command output = %+v", final.Output)
			}

			if test.err != nil && (final.Failure == nil || final.Failure.Message != test.err.Error()) {
				t.Fatalf("command failure = %+v", final.Failure)
			}

			if test.reason == apidebugger.ReasonStep && (final.Failure == nil || !strings.Contains(final.Failure.Message, "application disconnected")) {
				t.Fatalf("failure at second stop = %+v", final.Failure)
			}

			if _, ok := <-watch.Events; ok {
				t.Fatal("duplicate terminal event")
			}
		})
	}
}

func TestCommittedStopAndCloseWinConnectionLoss(t *testing.T) {
	for _, which := range []string{"stop", "close"} {
		t.Run(which, func(t *testing.T) {
			s, spy, loss, watch := connectionSession(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			spy.closeFn = func() error {
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			if _, err := s.start(ctx); err != nil {
				t.Fatal(err)
			}

			expectConnectionEvent(t, watch, EventRunning)
			expectConnectionEvent(t, watch, EventStopped)
			switch which {
			case "stop":
				s.requestTermination()
			case "close":
				if !s.beginClose() {
					t.Fatal("close not committed")
				}

				// Exercise the interval between Close admission and its worker.
				close(loss)
				go func() {
					s.settleClose()
					s.completeClose()
				}()
			}

			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			if which == "stop" {
				close(loss)
			}

			close(release)
			expectConnectionEvent(t, watch, EventTerminated)

			if _, ok := <-watch.Events; ok {
				t.Fatal("duplicate terminal event")
			}
		})
	}
}

func expectConnectionEvent(t *testing.T, watch Subscription, kind EventKind) SessionSnapshot {
	t.Helper()
	select {
	case event, ok := <-watch.Events:
		if !ok || event.Kind != kind {
			t.Fatalf("event = %+v, want %v", event, kind)
		}

		return event.Snapshot
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for event %v", kind)

		return SessionSnapshot{}
	}
}
