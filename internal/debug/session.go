package debug

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/lifecycle"
)

type (
	// session owns one exec.DebugRuntime and its retained debugger-specific state.
	// Its mutex precedes the embedded close operation when both locks are required
	// for a transition.
	session struct {
		mu        sync.Mutex
		controlMu sync.Mutex

		id               SessionID
		runtime          *exec.DebugRuntime
		state            State
		reason           apidebugger.Reason
		location         apisource.Location
		hitBreakpointIDs []apidebugger.BreakpointID
		output           *api.Output
		failure          *exec.RuntimeFailure
		breakpoints      map[string][]apidebugger.Breakpoint
		terminating      bool
		close            lifecycle.CloseOperation
		terminalDone     chan struct{}
		sequence         uint64
		lastEvent        Event
		nextWatcher      uint64
		watchers         map[uint64]*debugEventWatcher
	}

	debugEventWatcher struct {
		events chan Event
		errors chan error
		closed bool
	}
)

const watcherBufferSize = 8

func newSession(
	id SessionID,
	runtime *exec.DebugRuntime,
) *session {
	result := &session{
		id:           id,
		runtime:      runtime,
		state:        StateCreated,
		breakpoints:  make(map[string][]apidebugger.Breakpoint),
		terminalDone: make(chan struct{}),
		watchers:     make(map[uint64]*debugEventWatcher),
	}
	result.publishLocked(EventCreated, false)

	return result
}

func (d *session) snapshot() SessionSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

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

// replaceBreakpoints replaces every breakpoint for one source.
func (d *session) replaceBreakpoints(
	ctx context.Context,
	sourceName string,
	locations []apisource.Position,
) ([]apidebugger.Breakpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireInspectable(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	existing := append([]apidebugger.Breakpoint(nil), d.breakpoints[sourceName]...)
	d.mu.Unlock()

	for _, breakpoint := range existing {
		if err := d.runtime.Debugger().DeleteBreakpoint(breakpoint.ID); err != nil {
			return nil, err
		}
	}

	d.mu.Lock()
	d.breakpoints[sourceName] = nil
	d.mu.Unlock()

	bound := make([]apidebugger.Breakpoint, 0, len(locations))
	for _, location := range locations {
		breakpoint, err := d.runtime.Debugger().SetBreakpointAt(
			apisource.Location{
				SourceName: sourceName,
				Position:   location,
			},
			apidebugger.BreakpointOptions{
				BindingMode: apidebugger.BreakpointBindNextExecutableInSource,
			},
		)
		if err != nil {
			d.mu.Lock()
			d.breakpoints[sourceName] = bound
			d.mu.Unlock()

			return nil, err
		}

		bound = append(bound, breakpoint)
	}

	d.mu.Lock()
	d.breakpoints[sourceName] = bound
	d.mu.Unlock()

	return bound, nil
}

// frames returns the paused frame stack in current-to-caller order.
func (d *session) frames(ctx context.Context) ([]apidebugger.Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	return d.runtime.Debugger().Frames()
}

// scopes returns Locals and Parameters for one paused frame.
func (d *session) scopes(ctx context.Context, frame int) ([]Scope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	variables, err := d.runtime.Debugger().FrameLocals(frame)
	if err != nil {
		return nil, err
	}

	locals := Scope{Kind: ScopeLocals, Name: "Locals"}
	parameters := Scope{Kind: ScopeParameters, Name: "Parameters"}
	for _, variable := range variables {
		if variable.Param {
			parameters.Variables = append(parameters.Variables, variable)
		} else {
			locals.Variables = append(locals.Variables, variable)
		}
	}

	return []Scope{locals, parameters}, nil
}

// variables expands one value reference from the current paused state.
func (d *session) variables(
	ctx context.Context,
	reference apidebugger.ValueReference,
) ([]apidebugger.Variable, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	return d.runtime.Debugger().Variables(reference)
}

// evaluate evaluates a side-effect-free expression in one paused frame.
func (d *session) evaluate(
	ctx context.Context,
	frame int,
	expression string,
) (apidebugger.Value, error) {
	if err := ctx.Err(); err != nil {
		return apidebugger.Value{}, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return apidebugger.Value{}, err
	}

	value, err := d.runtime.Debugger().EvaluateFrame(ctx, frame, expression)
	if err != nil {
		return apidebugger.Value{}, err
	}

	return value, nil
}

// terminateExecution idempotently requests runtime termination while retaining the resource.
func (d *session) terminateExecution(ctx context.Context) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	d.requestTermination()

	return d.snapshot(), nil
}

// subscribe returns the latest lifecycle event and future bounded observations.
func (d *session) subscribe() Subscription {
	d.mu.Lock()
	current := d.lastEvent.clone()
	if d.state.Terminal() {
		events := make(chan Event)
		errorsChannel := make(chan error)
		close(events)
		close(errorsChannel)
		d.mu.Unlock()

		return Subscription{Current: current, Events: events, Errors: errorsChannel, Cancel: func() {}}
	}

	d.nextWatcher++
	id := d.nextWatcher
	watcher := &debugEventWatcher{
		events: make(chan Event, watcherBufferSize),
		errors: make(chan error, 1),
	}
	d.watchers[id] = watcher
	d.mu.Unlock()

	var once sync.Once

	return Subscription{
		Current: current,
		Events:  watcher.events,
		Errors:  watcher.errors,
		Cancel: func() {
			once.Do(func() { d.unsubscribe(id) })
		},
	}
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

func (d *session) requireInspectable() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == StateRunning {
		return ErrSessionRunning
	}

	if d.state.Terminal() || d.close.Started() {
		return ErrSessionTerminal
	}

	return nil
}

func (d *session) requireStopped() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == StateRunning {
		return ErrSessionRunning
	}

	if d.state != StateStopped || d.close.Started() {
		return ErrSessionNotStopped
	}

	return nil
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

func (d *session) snapshotLocked() SessionSnapshot {
	result := SessionSnapshot{
		ID:               d.id,
		ExecutionSession: d.runtime.SessionID(),
		State:            d.state,
		Reason:           d.reason,
		Location:         d.location,
		HitBreakpointIDs: append([]apidebugger.BreakpointID(nil), d.hitBreakpointIDs...),
		Parameters:       d.runtime.Parameters(),
		Options:          d.runtime.Options(),
		Output:           d.output,
		Failure:          d.failure.Clone(),
	}

	return result.Clone()
}

func (d *session) publishLocked(kind EventKind, terminal bool) {
	d.sequence++
	d.lastEvent = Event{
		Session:  d.id,
		Sequence: d.sequence,
		Kind:     kind,
		Snapshot: d.snapshotLocked(),
	}

	for id, watcher := range d.watchers {
		select {
		case watcher.events <- d.lastEvent.clone():
			if terminal {
				d.closeWatcherLocked(id, watcher, nil)
			}
		default:
			d.closeWatcherLocked(id, watcher, ErrWatcherLagged)
		}
	}

	if terminal {
		close(d.terminalDone)
	}
}

func (d *session) unsubscribe(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	watcher, ok := d.watchers[id]
	if !ok {
		return
	}

	d.closeWatcherLocked(id, watcher, nil)
}

func (d *session) closeWatcherLocked(id uint64, watcher *debugEventWatcher, err error) {
	if watcher.closed {
		return
	}

	watcher.closed = true
	if err != nil {
		watcher.errors <- err
	}

	close(watcher.events)
	close(watcher.errors)
	delete(d.watchers, id)
}
