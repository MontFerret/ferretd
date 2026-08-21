package exec

import (
	"context"
	"errors"
	"testing"
)

func TestWatchExecutionCurrentStateMultipleWatchersAndTerminalEOF(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	first, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("first WatchExecution: %v", err)
	}
	second, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("second WatchExecution: %v", err)
	}
	first.Cancel()

	if _, err := fixture.manager.RunExecution(context.Background(), created.ID); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	var terminal Event
	for event := range second.Events {
		terminal = event
	}
	second.Cancel()
	if terminal.Kind != EventCompleted || terminal.Sequence != 3 {
		t.Fatalf("terminal = %+v", terminal)
	}

	late, err := fixture.manager.WatchExecution(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("late WatchExecution: %v", err)
	}
	if late.Current.Kind != EventCompleted || late.Current.Sequence != 3 {
		t.Fatalf("late current = %+v", late.Current)
	}
	if _, ok := <-late.Events; ok {
		t.Fatal("terminal late watcher events channel is open")
	}
	if _, ok := <-late.Errors; ok {
		t.Fatal("terminal late watcher errors channel is open")
	}
}

func TestWatcherOverflowDisconnectsOnlyLaggingWatcher(t *testing.T) {
	fixture := newExecutionFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateExecution(
		context.Background(),
		fixture.session.ID,
		nil,
		RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	execution := retainedExecution(t, fixture.manager, created.ID).execution
	lagging := execution.subscribe()
	independent := execution.subscribe()
	defer independent.Cancel()

	for range watcherBufferSize + 1 {
		execution.mu.Lock()
		execution.publishLocked(EventStarted, false)
		execution.mu.Unlock()
		<-independent.Events
	}

	var got error
	for watchErr := range lagging.Errors {
		got = watchErr
	}
	if !errors.Is(got, ErrWatcherLagged) {
		t.Fatalf("lag error = %v, want ErrWatcherLagged", got)
	}

	execution.mu.Lock()
	_, lagStillRegistered := execution.watchers[1]
	_, independentRegistered := execution.watchers[2]
	execution.mu.Unlock()
	if lagStillRegistered || !independentRegistered {
		t.Fatalf("watcher registration: lagging=%t independent=%t", lagStillRegistered, independentRegistered)
	}
}
