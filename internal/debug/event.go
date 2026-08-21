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
	// EventCreated publishes the initial non-running Session snapshot.
	EventCreated EventKind = iota + 1
	// EventRunning publishes entry into active debugger execution.
	EventRunning
	// EventStopped publishes a debugger-visible suspension and its inspection state.
	EventStopped
	// EventCompleted publishes successful terminal execution.
	EventCompleted
	// EventFailed publishes terminal execution with a retained runtime failure.
	EventFailed
	// EventTerminated publishes explicit terminal cancellation by the debugger owner.
	EventTerminated
)

func (e Event) clone() Event {
	e.Snapshot = e.Snapshot.Clone()

	return e
}
