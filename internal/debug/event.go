package debug

type (
	// EventKind identifies an ordered debug Session lifecycle event.
	EventKind uint8

	// Event is one ordered lifecycle observation for a debug Session.
	Event struct {
		Session  SessionID
		Sequence uint64
		Kind     EventKind
		Snapshot SessionSnapshot
	}

	// Subscription provides the latest event and bounded future observations.
	Subscription struct {
		Current Event
		Events  <-chan Event
		Errors  <-chan error
		Cancel  func()
	}
)

const (
	EventCreated EventKind = iota + 1
	EventRunning
	EventStopped
	EventCompleted
	EventFailed
	EventTerminated
)

func (e Event) clone() Event {
	e.Snapshot = e.Snapshot.Clone()

	return e
}
