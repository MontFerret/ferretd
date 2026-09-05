package debug

import (
	"sync"
)

// subscribe returns the latest lifecycle event and future bounded observations.
func (d *session) subscribe() Subscription {
	d.mu.Lock()

	current := d.lastEvent.clone()
	if d.state.Terminal() {
		events := make(chan Event)
		errorsChannel := make(chan error)
		close(events)
		close(errorsChannel)
		d.mu.Unlock()

		return Subscription{Current: current, Events: events, Errors: errorsChannel, Cancel: func() {}}
	}

	d.nextWatcher++
	id := d.nextWatcher
	watcher := &debugEventWatcher{
		events: make(chan Event, watcherBufferSize),
		errors: make(chan error, 1),
	}
	d.watchers[id] = watcher
	d.mu.Unlock()

	var once sync.Once

	return Subscription{
		Current: current,
		Events:  watcher.events,
		Errors:  watcher.errors,
		Cancel: func() {
			once.Do(func() { d.unsubscribe(id) })
		},
	}
}

func (d *session) publishLocked(kind EventKind, terminal bool) {
	d.sequence++
	d.lastEvent = Event{
		Session:  d.id,
		Sequence: d.sequence,
		Kind:     kind,
		Snapshot: d.snapshotLocked(),
	}

	for id, watcher := range d.watchers {
		select {
		case watcher.events <- d.lastEvent.clone():
			if terminal {
				d.closeWatcherLocked(id, watcher, nil)
			}
		default:
			d.closeWatcherLocked(id, watcher, ErrWatcherLagged)
		}
	}

	if terminal {
		close(d.terminalDone)
	}
}

func (d *session) unsubscribe(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	watcher, ok := d.watchers[id]
	if !ok {
		return
	}

	d.closeWatcherLocked(id, watcher, nil)
}

func (d *session) closeWatcherLocked(id uint64, watcher *debugEventWatcher, err error) {
	if watcher.closed {
		return
	}

	watcher.closed = true

	if err != nil {
		watcher.errors <- err
	}

	close(watcher.events)
	close(watcher.errors)
	delete(d.watchers, id)
}
