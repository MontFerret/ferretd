package exec

// Event is one ordered lifecycle observation for an Execution.
type Event struct {
	Execution ExecutionID
	Sequence  uint64
	Kind      EventKind
	Snapshot  ExecutionSnapshot
}

// Subscription provides the current event and bounded future observations.
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
