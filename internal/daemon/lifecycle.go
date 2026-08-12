package daemon

type lifecycleState uint8

const (
	stateNew lifecycleState = iota
	stateStarting
	stateRunning
	stateStopping
	stateStopped
)
