package debug

import (
	"errors"

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
	case apidebugger.ReasonEntry:
		d.stopLocked(apidebugger.ReasonEntry)
	case apidebugger.ReasonBreakpoint:
		d.hitBreakpointIDs = append([]apidebugger.BreakpointID(nil), event.HitBreakpointIDs...)
		d.stopLocked(apidebugger.ReasonBreakpoint)
	case apidebugger.ReasonStep:
		d.stopLocked(apidebugger.ReasonStep)
	case apidebugger.ReasonPause:
		d.stopLocked(apidebugger.ReasonPause)
	case apidebugger.ReasonRuntimeError:
		d.reason = apidebugger.ReasonRuntimeError
		d.state = StateStopped
		d.failure = d.runtime.MaterializeFailure(event.Error)
		d.publishLocked(EventStopped, false)
	case apidebugger.ReasonCompleted:
		d.state = StateCompleted
		d.output = d.runtime.MaterializeOutput(event.Output)
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

func (d *session) stopLocked(reason apidebugger.Reason) {
	d.reason = reason
	d.failure = nil
	d.state = StateStopped
	d.publishLocked(EventStopped, false)
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
