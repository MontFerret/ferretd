package debug

type debugEventWatcher struct {
	events chan Event
	errors chan error
	closed bool
}

const watcherBufferSize = 8
