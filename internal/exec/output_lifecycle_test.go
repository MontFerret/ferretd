package exec

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MontFerret/api"
)

func TestExecutionWaitsForCollectionAndOwnedCleanup(t *testing.T) {
	collectStarted := make(chan struct{})
	releaseCollect := make(chan struct{})
	outputCloseStarted := make(chan struct{})
	releaseOutputClose := make(chan struct{})
	sessionCloseStarted := make(chan struct{})
	releaseSessionClose := make(chan struct{})
	planCloseStarted := make(chan struct{})
	collectContext := make(chan context.Context, 1)
	finishCollect := sync.OnceFunc(func() { close(releaseCollect) })
	finishOutputClose := sync.OnceFunc(func() { close(releaseOutputClose) })
	finishSessionClose := sync.OnceFunc(func() { close(releaseSessionClose) })

	manager, session, runtime := newHookedManager(t, "RETURN 1",
		withSessionCloseHook(func() error {
			close(sessionCloseStarted)
			<-releaseSessionClose

			return nil
		}),
		withPlanCloseHook(func() error {
			close(planCloseStarted)

			return nil
		}),
	)
	// Release blocked runtime work before fixture cleanup if an assertion fails.
	t.Cleanup(finishSessionClose)
	t.Cleanup(finishOutputClose)
	t.Cleanup(finishCollect)
	output := &outputSpy{
		collectFn: func(ctx context.Context) (*api.Content, error) {
			collectContext <- ctx
			close(collectStarted)
			<-releaseCollect

			return &api.Content{Data: []byte("result")}, nil
		},
		closeFn: func() error {
			close(outputCloseStarted)
			<-releaseOutputClose

			return nil
		},
	}
	runtime.runFn = func(context.Context) (api.Output, error) { return output, nil }

	created, err := manager.CreateExecution(t.Context(), session.ID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := manager.WatchExecution(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(subscription.Cancel)

	invocation, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	if _, err := manager.RunExecution(invocation, created.ID); err != nil {
		t.Fatal(err)
	}

	if event := nextOutputEvent(t, subscription); event.Kind != EventStarted {
		t.Fatalf("event = %+v, want started", event)
	}

	waitForSignal(t, collectStarted, "collection")
	cancel()

	if ctx := <-collectContext; ctx.Err() != nil {
		t.Fatalf("RPC cancellation reached collection: %v", ctx.Err())
	}

	entry := retainedExecution(t, manager, created.ID)
	parent := retainedSession(t, manager, session.ID)

	if state := entry.execution.snapshot().State; state != StateRunning {
		t.Fatalf("state during collection = %v", state)
	}

	assertNoOutputEvent(t, subscription)
	finishCollect()
	waitForSignal(t, outputCloseStarted, "output cleanup")
	assertNoSignal(t, sessionCloseStarted, "session closed before output settled")
	assertNoOutputEvent(t, subscription)

	parentClose := make(chan error, 1)
	go func() { parentClose <- manager.CloseSession(context.Background(), session.ID) }()
	waitForSessionClosing(t, parent.session)
	assertNoSignal(t, planCloseStarted, "plan closed before output settled")

	finishOutputClose()
	waitForSignal(t, sessionCloseStarted, "session cleanup")
	assertNoSignal(t, planCloseStarted, "plan closed before session settled")
	assertNoOutputEvent(t, subscription)

	if state := entry.execution.snapshot().State; state != StateRunning {
		t.Fatalf("state during session cleanup = %v", state)
	}

	finishSessionClose()

	terminal := nextOutputEvent(t, subscription)
	if terminal.Kind != EventCompleted || terminal.Sequence != 3 || string(terminal.Snapshot.Output.Data) != "result" {
		t.Fatalf("terminal event = %+v", terminal)
	}

	waitForResult(t, parentClose, "parent close")

	if _, ok := <-subscription.Events; ok {
		t.Fatal("additional terminal event")
	}

	if output.collects.Load() != 1 || output.closes.Load() != 1 {
		t.Fatalf("collection/cleanup counts = %d/%d", output.collects.Load(), output.closes.Load())
	}
}

func TestOutputFailurePhasesAndRetainedCleanup(t *testing.T) {
	primary := errors.New("primary failure")
	outputCleanup := errors.New("output cleanup")
	sessionCleanup := errors.New("session cleanup")
	for _, test := range []struct {
		name       string
		runErr     error
		collectErr error
		nilOutput  bool
		category   FailureCategory
	}{
		{name: "collection", collectErr: primary, category: FailureRuntime},
		{name: "collection cleanup", category: FailureRuntime},
		{name: "run", runErr: primary, category: FailureRuntime},
		{name: "invalid nil output", nilOutput: true, category: FailureRuntime},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, session, runtime := newHookedManager(t, "RETURN 1", withSessionCloseHook(func() error {
				return sessionCleanup
			}))
			content := &api.Content{Metadata: api.Metadata{ContentType: "text/plain", Length: 100, LengthKnown: true}, Data: []byte("prefix")}
			output := &outputSpy{
				collectFn: func(context.Context) (*api.Content, error) { return content, test.collectErr },
				closeFn:   func() error { return outputCleanup },
			}
			runtime.runFn = func(context.Context) (api.Output, error) {
				if test.nilOutput {
					return nil, nil
				}

				return output, test.runErr
			}

			created, err := manager.CreateExecution(t.Context(), session.ID, nil, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			owner := retainedExecution(t, manager, created.ID).execution.runtime

			result := owner.run()
			if result.category != test.category || result.err == nil || !errors.Is(result.err, sessionCleanup) {
				t.Fatalf("result = %+v", result)
			}

			if !test.nilOutput && !errors.Is(result.err, outputCleanup) {
				t.Fatalf("missing output cleanup: %v", result.err)
			}

			if (test.runErr != nil || test.collectErr != nil) && !errors.Is(result.err, primary) {
				t.Fatalf("lost primary failure: %v", result.err)
			}

			if test.runErr == nil && !test.nilOutput {
				if result.output != content {
					t.Fatal("detached content was discarded or copied")
				}
			} else if result.output != nil || output.collects.Load() != 0 {
				t.Fatal("failed admission consumed output")
			}

			for range 2 {
				closeErr := owner.closeResources()
				if !errors.Is(closeErr, sessionCleanup) || (!test.nilOutput && !errors.Is(closeErr, outputCleanup)) {
					t.Fatalf("retained cleanup = %v", closeErr)
				}
			}

			if !test.nilOutput && output.closes.Load() != 1 {
				t.Fatalf("output cleanup count = %d", output.closes.Load())
			}
		})
	}
}

func TestExecutionCancellationSettlesReturnedOutput(t *testing.T) {
	for _, beforeAdmission := range []bool{false, true} {
		t.Run(map[bool]string{false: "during collection", true: "before collection admission"}[beforeAdmission], func(t *testing.T) {
			outputCleanup, sessionCleanup := errors.New("output cleanup"), errors.New("session cleanup")
			manager, session, runtime := newHookedManager(t, "RETURN 1", withSessionCloseHook(func() error {
				return sessionCleanup
			}))
			collectStarted := make(chan struct{})
			output := &outputSpy{
				collectFn: func(ctx context.Context) (*api.Content, error) {
					close(collectStarted)
					<-ctx.Done()

					return &api.Content{Data: []byte("prefix")}, ctx.Err()
				},
				closeFn: func() error { return outputCleanup },
			}

			created, err := manager.CreateExecution(t.Context(), session.ID, nil, RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			owner := retainedExecution(t, manager, created.ID).execution.runtime
			runtime.runFn = func(context.Context) (api.Output, error) {
				if beforeAdmission {
					owner.cancel(errExecutionCanceled)
				}

				return output, nil
			}

			subscription, err := manager.WatchExecution(t.Context(), created.ID)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(subscription.Cancel)

			if _, err := manager.RunExecution(t.Context(), created.ID); err != nil {
				t.Fatal(err)
			}

			if event := nextOutputEvent(t, subscription); event.Kind != EventStarted {
				t.Fatalf("first event = %+v", event)
			}

			if !beforeAdmission {
				waitForSignal(t, collectStarted, "collection")

				if _, err := manager.CancelExecution(t.Context(), created.ID); err != nil {
					t.Fatal(err)
				}
			}

			terminal := nextOutputEvent(t, subscription)
			if terminal.Kind != EventCancelled || terminal.Sequence != 3 {
				t.Fatalf("terminal = %+v", terminal)
			}

			if !beforeAdmission && (terminal.Snapshot.Output == nil || string(terminal.Snapshot.Output.Data) != "prefix") {
				t.Fatalf("cancelled output = %+v", terminal.Snapshot.Output)
			}

			if _, ok := <-subscription.Events; ok {
				t.Fatal("additional terminal event")
			}

			if beforeAdmission && output.collects.Load() != 0 {
				t.Fatal("cancelled collection was admitted")
			}

			if err := manager.CloseSession(t.Context(), session.ID); !errors.Is(err, outputCleanup) || !errors.Is(err, sessionCleanup) {
				t.Fatalf("parent cleanup = %v", err)
			}

			if output.closes.Load() != 1 {
				t.Fatalf("output cleanup count = %d", output.closes.Load())
			}
		})
	}
}

func TestCollectedContentIsolatedAcrossWatchers(t *testing.T) {
	manager, session, runtime := newHookedManager(t, "RETURN 1")
	metadata := api.Metadata{ContentType: "text/plain", Length: 100, LengthKnown: true}
	runtime.collect = func(context.Context, sessionOptionsSpy) (*api.Content, error) {
		return &api.Content{Metadata: metadata, Data: []byte("prefix")}, errors.New("delivery failed")
	}

	created, err := manager.CreateExecution(t.Context(), session.ID, nil, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	first, err := manager.WatchExecution(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(first.Cancel)

	second, err := manager.WatchExecution(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(second.Cancel)

	if _, err := manager.RunExecution(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	_ = nextOutputEvent(t, first)
	_ = nextOutputEvent(t, second)

	one, two := nextOutputEvent(t, first), nextOutputEvent(t, second)
	if one.Kind != EventFailed || two.Kind != EventFailed || one.Snapshot.Failure.Category != FailureRuntime {
		t.Fatalf("terminal events = %+v, %+v", one, two)
	}

	one.Snapshot.Output.Metadata.Length = 1
	one.Snapshot.Output.Data[0] = 'X'

	retained, err := manager.GetExecution(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, content := range []*api.Content{two.Snapshot.Output, retained.Output} {
		if content.Metadata != metadata || string(content.Data) != "prefix" {
			t.Fatalf("observer content = %+v", content)
		}
	}
}

func nextOutputEvent(t *testing.T, subscription Subscription) Event {
	t.Helper()

	select {
	case event, ok := <-subscription.Events:
		if !ok {
			t.Fatal("watch ended before expected event")
		}

		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for execution event")

		return Event{}
	}
}

func assertNoOutputEvent(t *testing.T, subscription Subscription) {
	t.Helper()

	select {
	case event := <-subscription.Events:
		t.Fatalf("event before settlement: %+v", event)
	default:
	}
}
