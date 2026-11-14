package debug

import (
	"errors"
	"fmt"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

func (d *session) applyRuntimeEventLocked(event *apidebugger.Event, runtimeErrorResume bool) {
	if event == nil {
		d.failLocked(errors.New("debug execution returned no event"))

		return
	}

	d.location = event.Location.Location
	switch event.Reason {
	case apidebugger.ReasonEntry, apidebugger.ReasonBreakpoint, apidebugger.ReasonStep,
		apidebugger.ReasonPause, apidebugger.ReasonRuntimeError:
		d.stopLocked(event)
	case apidebugger.ReasonCompleted:
		d.state = StateCompleted
		d.output = cloneContent(event.Output)
		d.publishLocked(EventCompleted, true)
	case apidebugger.ReasonTerminated:
		if runtimeErrorResume && event.Error != nil && !d.terminating {
			d.failLocked(event.Error)
		} else {
			d.state = StateTerminated
			d.publishLocked(EventTerminated, true)
		}
	default:
		d.failLocked(errors.New("debug execution returned an unknown event"))
	}
}

func (d *session) stopLocked(event *apidebugger.Event) {
	if d.connection != nil {
		if d.failConnectionLocked() {
			return
		}

		if !d.watchingConnection {
			d.watchingConnection = true

			go d.observeConnection(d.connection.ConnectionDone())
		}
	}

	d.reason = event.Reason
	d.failure = nil

	if event.Reason == apidebugger.ReasonRuntimeError {
		d.failure = d.runtime.MaterializeFailure(event.Error)
	}

	if event.Reason == apidebugger.ReasonBreakpoint {
		d.hitBreakpointIDs = append([]apidebugger.BreakpointID(nil), event.HitBreakpointIDs...)
	}

	d.state = StateStopped
	d.publishLocked(EventStopped, false)
}

// While running, the command owns its result, including any partial output.
// Every subsequent stop also checks the retained connection outcome under mu.
func (d *session) observeConnection(done <-chan struct{}) {
	select {
	case <-d.terminalDone:
		return
	case <-done:
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == StateStopped && !d.terminating && !d.close.Started() {
		d.failConnectionLocked()
	}
}

func (d *session) failConnectionLocked() bool {
	err := d.connection.ConnectionError()
	if err == nil {
		return false
	}

	d.reason = ""
	d.hitBreakpointIDs = nil
	d.failLocked(fmt.Errorf("lost connection to Wire runtime %q: %w", d.runtimeName, err))

	return true
}

func (d *session) failLocked(err error) {
	d.state = StateFailed
	d.failure = d.runtime.MaterializeFailure(err)
	d.publishLocked(EventFailed, true)
}

func (d *session) requestTermination() {
	d.mu.Lock()
	if d.state.Terminal() || d.terminating {
		d.mu.Unlock()

		return
	}

	d.terminating = true
	d.mu.Unlock()

	go d.terminate()
}

func (d *session) terminate() {
	_ = d.runtime.Close()

	d.mu.Lock()
	if !d.state.Terminal() {
		d.state = StateTerminated
		d.reason = ""
		d.location = apisource.Location{}
		d.failure = nil
		d.publishLocked(EventTerminated, true)
	}

	d.mu.Unlock()
}

func (d *session) beginClose() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.close.Begin()
}

func (d *session) settleClose() {
	d.requestTermination()
	<-d.terminalDone
	_ = d.runtime.Close()

	d.mu.Lock()
	for id, watcher := range d.watchers {
		d.closeWatcherLocked(id, watcher, nil)
	}

	d.mu.Unlock()
}

func (d *session) completeClose() {
	d.close.Finish(d.runtime.Close())
}
