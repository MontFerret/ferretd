package exec

type (
	// EventKind identifies a strongly typed execution lifecycle event.
	EventKind uint8

	// Event is one ordered lifecycle observation for an Execution.
	Event struct {
		Execution ExecutionID
		Sequence  uint64
		Kind      EventKind
		Snapshot  ExecutionSnapshot
	}

	// Subscription provides the current event and bounded future observations.
	Subscription struct {
		Current Event
		Events  <-chan Event
		Errors  <-chan error
		Cancel  func()
	}
)

const (
	// EventCreated reports creation of an Execution resource.
	EventCreated EventKind = iota + 1
	// EventStarted reports the start of the one-shot invocation.
	EventStarted
	// EventCompleted reports successful termination.
	EventCompleted
	// EventFailed reports failed termination.
	EventFailed
	// EventCancelled reports cancellation termination.
	EventCancelled
)

func (e Event) clone() Event {
	e.Snapshot = e.Snapshot.Clone()

	return e
}
