package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferretd/internal/lifecycle"
)

type (
	// Execution owns one isolated, one-shot Ferret invocation.
	Execution struct {
		mu sync.Mutex

		id          ExecutionID
		runtime     *executionRuntime
		state       State
		output      *Output
		failure     *Failure
		runDone     chan struct{}
		close       lifecycle.CloseOperation
		sequence    uint64
		lastEvent   Event
		nextWatcher uint64
		watchers    map[uint64]*eventWatcher
	}

	eventWatcher struct {
		events chan Event
		errors chan error
		closed bool
	}
)

const watcherBufferSize = 8

func newExecution(
	id ExecutionID,
	runtime *executionRuntime,
) *Execution {
	result := &Execution{
		id:       id,
		runtime:  runtime,
		state:    StateCreated,
		runDone:  make(chan struct{}),
		watchers: make(map[uint64]*eventWatcher),
	}
	result.publishLocked(EventCreated, false)

	return result
}

// Snapshot returns an immutable Execution view.
func (e *Execution) Snapshot() ExecutionSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.snapshotLocked()
}

// Start commits RUNNING and starts the daemon-owned one-shot invocation.
func (e *Execution) Start(ctx context.Context) (ExecutionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionSnapshot{}, err
	}

	e.mu.Lock()
	if err := ctx.Err(); err != nil {
		e.mu.Unlock()

		return ExecutionSnapshot{}, err
	}

	switch e.state {
	case StateCreated:
		e.state = StateRunning
		e.publishLocked(EventStarted, false)
		snapshot := e.snapshotLocked()
		e.mu.Unlock()

		go e.run()

		return snapshot, nil
	case StateRunning:
		e.mu.Unlock()

		return ExecutionSnapshot{}, ErrExecutionRunning
	default:
		e.mu.Unlock()

		return ExecutionSnapshot{}, ErrExecutionTerminal
	}
}

// Cancel idempotently requests cancellation without overwriting terminal state.
func (e *Execution) Cancel() ExecutionSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	switch e.state {
	case StateCreated:
		e.runtime.cancel(errExecutionCanceled)
		e.state = StateCancelled
		e.publishLocked(EventCancelled, true)
	case StateRunning:
		e.runtime.cancel(errExecutionCanceled)
	}

	return e.snapshotLocked()
}

// Subscribe returns the latest lifecycle event and future bounded observations.
func (e *Execution) Subscribe() Subscription {
	e.mu.Lock()
	current := e.lastEvent.clone()
	if e.state.Terminal() {
		events := make(chan Event)
		errors := make(chan error)
		close(events)
		close(errors)
		e.mu.Unlock()

		return Subscription{Current: current, Events: events, Errors: errors, Cancel: func() {}}
	}

	e.nextWatcher++
	id := e.nextWatcher
	watcher := &eventWatcher{
		events: make(chan Event, watcherBufferSize),
		errors: make(chan error, 1),
	}
	e.watchers[id] = watcher
	e.mu.Unlock()

	var once sync.Once

	return Subscription{
		Current: current,
		Events:  watcher.events,
		Errors:  watcher.errors,
		Cancel: func() {
			once.Do(func() { e.unsubscribe(id) })
		},
	}
}

func (e *Execution) run() {
	result := e.runtime.run()
	e.finish(result.output, result.err, result.category)
}

func (e *Execution) finish(output *RuntimeOutput, err error, category FailureCategory) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != StateRunning {
		return
	}

	e.output = output
	if err == nil {
		e.state = StateCompleted
		e.publishLocked(EventCompleted, true)

		return
	}

	if errors.Is(err, context.Canceled) && context.Cause(e.runtime.ctx) != nil {
		e.state = StateCancelled
		e.publishLocked(EventCancelled, true)

		return
	}

	e.state = StateFailed
	e.failure = &Failure{Category: category, RuntimeFailure: *e.runtime.failure(err)}
	e.publishLocked(EventFailed, true)
}

func (e *Execution) beginClose() bool {
	return e.close.Begin()
}

func (e *Execution) settleClose() {
	e.Cancel()
	<-e.runDone
	_ = e.runtime.closeSession()

	e.mu.Lock()
	for id, watcher := range e.watchers {
		e.closeWatcherLocked(id, watcher, nil)
	}

	e.mu.Unlock()
}

func (e *Execution) completeClose() {
	e.close.Finish(nil)
}

func (e *Execution) snapshotLocked() ExecutionSnapshot {
	return (ExecutionSnapshot{
		ID:         e.id,
		Session:    e.runtime.target.session,
		State:      e.state,
		Parameters: e.runtime.input.parameters,
		Options:    e.runtime.input.options,
		Output:     e.output,
		Failure:    e.failure,
	}).Clone()
}

func (e *Execution) publishLocked(kind EventKind, terminal bool) {
	e.sequence++
	e.lastEvent = Event{
		Execution: e.id,
		Sequence:  e.sequence,
		Kind:      kind,
		Snapshot:  e.snapshotLocked(),
	}

	for id, watcher := range e.watchers {
		select {
		case watcher.events <- e.lastEvent.clone():
			if terminal {
				e.closeWatcherLocked(id, watcher, nil)
			}
		default:
			e.closeWatcherLocked(id, watcher, ErrWatcherLagged)
		}
	}

	if terminal {
		close(e.runDone)
	}
}

func (e *Execution) unsubscribe(id uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	watcher, ok := e.watchers[id]
	if !ok {
		return
	}

	e.closeWatcherLocked(id, watcher, nil)
}

func (e *Execution) closeWatcherLocked(id uint64, watcher *eventWatcher, err error) {
	if watcher.closed {
		return
	}

	watcher.closed = true

	if err != nil {
		watcher.errors <- err
	}

	close(watcher.events)
	close(watcher.errors)
	delete(e.watchers, id)
}
