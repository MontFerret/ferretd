package debug

// Event is one ordered lifecycle observation for a debug Session.
type Event struct {
	Session  SessionID
	Sequence uint64
	Kind     EventKind
	Snapshot SessionSnapshot
}

// Subscription provides the latest event and bounded future observations.
type Subscription struct {
	Current Event
	Events  <-chan Event
	Errors  <-chan error
	Cancel  func()
}

func (e Event) clone() Event {
	e.Snapshot = e.Snapshot.Clone()

	return e
}
