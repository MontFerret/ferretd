package debug

import (
	"context"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

// start commits RUNNING and asynchronously waits for the runtime's first stop.
func (d *session) start(ctx context.Context) (SessionSnapshot, error) {
	return d.startCommand(ctx, StateCreated, func() (*apidebugger.Event, error) {
		return d.runtime.Debugger().Start(d.runtime.Context())
	}, false)
}

// continueExecution resumes execution until the runtime reports its next event.
func (d *session) continueExecution(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().Continue)
}

// stepIn resumes and stops at the next logical source location.
func (d *session) stepIn(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().StepIn)
}

// stepOver resumes and stops at the next location in the same or a shallower frame.
func (d *session) stepOver(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().StepOver)
}

// stepOut resumes and stops in a caller frame.
func (d *session) stepOut(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().StepOut)
}

// pause requests a safe stop without waiting for the active command.
func (d *session) pause(ctx context.Context) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	d.mu.Lock()
	if d.state != StateRunning {
		d.mu.Unlock()

		return SessionSnapshot{}, ErrSessionNotRunning
	}
	d.mu.Unlock()

	if err := d.runtime.Debugger().Pause(); err != nil {
		return SessionSnapshot{}, err
	}

	return d.snapshot(), nil
}

// terminateExecution idempotently requests runtime termination while retaining the resource.
func (d *session) terminateExecution(ctx context.Context) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	d.requestTermination()

	return d.snapshot(), nil
}

func (d *session) resumeCommand(
	ctx context.Context,
	command func(context.Context) (*apidebugger.Event, error),
) (SessionSnapshot, error) {
	d.mu.Lock()
	runtimeErrorResume := d.state == StateStopped && d.reason == apidebugger.ReasonRuntimeError
	d.mu.Unlock()

	return d.startCommand(ctx, StateStopped, func() (*apidebugger.Event, error) {
		return command(d.runtime.Context())
	}, runtimeErrorResume)
}

func (d *session) startCommand(
	ctx context.Context,
	expected State,
	command func() (*apidebugger.Event, error),
	runtimeErrorResume bool,
) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	d.mu.Lock()
	if d.terminating || d.close.Started() {
		d.mu.Unlock()

		return SessionSnapshot{}, ErrSessionTerminal
	}

	if d.state != expected {
		actual := d.state
		d.mu.Unlock()

		if actual == StateRunning {
			return SessionSnapshot{}, ErrSessionRunning
		}

		return SessionSnapshot{}, ErrSessionTerminal
	}

	d.state = StateRunning
	d.reason = ""
	d.location = apisource.Location{}
	d.hitBreakpointIDs = nil
	d.failure = nil
	d.publishLocked(EventRunning, false)
	snapshot := d.snapshotLocked()
	d.mu.Unlock()

	go d.runCommand(command, runtimeErrorResume)

	return snapshot, nil
}

func (d *session) runCommand(command func() (*apidebugger.Event, error), runtimeErrorResume bool) {
	event, err := command()

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state != StateRunning {
		return
	}

	if err != nil {
		if d.terminating {
			d.state = StateTerminated
			d.publishLocked(EventTerminated, true)

			return
		}

		d.failLocked(err)

		return
	}

	d.applyRuntimeEventLocked(event, runtimeErrorResume)
}
