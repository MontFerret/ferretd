package debug

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/lifecycle"
)

type (
	// Session owns one retained Ferret debugger session. Its mutex precedes the
	// embedded close operation when both locks are required for a transition.
	Session struct {
		mu        sync.Mutex
		controlMu sync.Mutex

		id               SessionID
		runtime          *exec.DebugRuntime
		state            State
		reason           StopReason
		location         Location
		hitBreakpointIDs []uint64
		output           *Output
		failure          *Failure
		breakpoints      map[string][]ferret.DebugBreakpoint
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

func newSession(
	id SessionID,
	runtime *exec.DebugRuntime,
) *Session {
	result := &Session{
		id:           id,
		runtime:      runtime,
		state:        StateCreated,
		breakpoints:  make(map[string][]ferret.DebugBreakpoint),
		terminalDone: make(chan struct{}),
		watchers:     make(map[uint64]*debugEventWatcher),
	}
	result.publishLocked(EventCreated, false)

	return result
}

const watcherBufferSize = 8

// Snapshot returns an immutable Session view.
func (d *Session) Snapshot() SessionSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

// Start commits RUNNING and asynchronously waits for Ferret's first stop.
func (d *Session) Start(ctx context.Context) (SessionSnapshot, error) {
	return d.startCommand(ctx, StateCreated, func() (*ferret.DebugEvent, error) {
		return d.runtime.Debugger().Start(d.runtime.Context())
	}, false)
}

// Continue resumes execution until Ferret reports its next event.
func (d *Session) Continue(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().Continue)
}

// StepIn resumes and stops at the next logical source location.
func (d *Session) StepIn(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().Step)
}

// StepOver resumes and stops at the next location in the same or a shallower frame.
func (d *Session) StepOver(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().Next)
}

// StepOut resumes and stops in a caller frame.
func (d *Session) StepOut(ctx context.Context) (SessionSnapshot, error) {
	return d.resumeCommand(ctx, d.runtime.Debugger().Out)
}

// Pause requests a safe stop without waiting for the active command.
func (d *Session) Pause(ctx context.Context) (SessionSnapshot, error) {
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

	return d.Snapshot(), nil
}

// ReplaceBreakpoints replaces every breakpoint for one source file.
func (d *Session) ReplaceBreakpoints(
	ctx context.Context,
	file string,
	locations []BreakpointLocation,
) ([]Breakpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireInspectable(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	existing := append([]ferret.DebugBreakpoint(nil), d.breakpoints[file]...)
	d.mu.Unlock()

	for _, breakpoint := range existing {
		if err := d.runtime.Debugger().DeleteBreakpoint(breakpoint.ID); err != nil {
			return nil, err
		}
	}

	d.mu.Lock()
	d.breakpoints[file] = nil
	d.mu.Unlock()

	bound := make([]ferret.DebugBreakpoint, 0, len(locations))
	result := make([]Breakpoint, 0, len(locations))
	for _, location := range locations {
		breakpoint, err := d.runtime.Debugger().SetBreakpointAt(
			ferret.DebugSourceLocation{File: file, Line: location.Line, Column: location.Column},
			ferret.DebugBreakpointOptions{BindingMode: ferret.DebugBreakpointBindNextExecutableInFile},
		)
		if err != nil {
			d.mu.Lock()
			d.breakpoints[file] = bound
			d.mu.Unlock()

			return nil, err
		}

		bound = append(bound, breakpoint)
		result = append(result, convertBreakpoint(breakpoint))
	}

	d.mu.Lock()
	d.breakpoints[file] = bound
	d.mu.Unlock()

	return result, nil
}

// Frames returns the paused frame stack in current-to-caller order.
func (d *Session) Frames(ctx context.Context) ([]Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	frames, err := d.runtime.Debugger().Frames()
	if err != nil {
		return nil, err
	}

	result := make([]Frame, len(frames))
	for index, frame := range frames {
		result[index] = Frame{
			Index:    index,
			Name:     frame.Name,
			Location: convertLocation(frame.Location),
		}
	}

	return result, nil
}

// Scopes returns Locals and Parameters for one paused frame.
func (d *Session) Scopes(ctx context.Context, frame int) ([]Scope, error) {
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
		converted := convertVariable(variable)

		if variable.Param {
			parameters.Variables = append(parameters.Variables, converted)
		} else {
			locals.Variables = append(locals.Variables, converted)
		}
	}

	return []Scope{locals, parameters}, nil
}

// Variables expands one value reference from the current paused state.
func (d *Session) Variables(ctx context.Context, reference ValueReference) ([]Variable, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	variables, err := d.runtime.Debugger().Variables(ferret.DebugValueReference(reference))
	if err != nil {
		return nil, err
	}

	return convertVariables(variables), nil
}

// Evaluate evaluates a side-effect-free expression in one paused frame.
func (d *Session) Evaluate(ctx context.Context, frame int, expression string) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return Value{}, err
	}

	value, err := d.runtime.Debugger().EvaluateFrame(ctx, frame, expression)
	if err != nil {
		return Value{}, err
	}

	return convertValue(value), nil
}

// Terminate idempotently requests Ferret termination while retaining the resource.
func (d *Session) Terminate(ctx context.Context) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	d.requestTermination()

	return d.Snapshot(), nil
}

// Subscribe returns the latest lifecycle event and future bounded observations.
func (d *Session) Subscribe() Subscription {
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

func (d *Session) resumeCommand(
	ctx context.Context,
	command func(context.Context) (*ferret.DebugEvent, error),
) (SessionSnapshot, error) {
	d.mu.Lock()
	runtimeErrorResume := d.state == StateStopped && d.reason == StopRuntimeError
	d.mu.Unlock()

	return d.startCommand(ctx, StateStopped, func() (*ferret.DebugEvent, error) {
		return command(d.runtime.Context())
	}, runtimeErrorResume)
}

func (d *Session) startCommand(
	ctx context.Context,
	expected State,
	command func() (*ferret.DebugEvent, error),
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
	d.reason = StopNone
	d.location = Location{}
	d.hitBreakpointIDs = nil
	d.failure = nil
	d.publishLocked(EventRunning, false)
	snapshot := d.snapshotLocked()
	d.mu.Unlock()

	go d.runCommand(command, runtimeErrorResume)

	return snapshot, nil
}

func (d *Session) runCommand(command func() (*ferret.DebugEvent, error), runtimeErrorResume bool) {
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

	d.applyFerretEventLocked(event, runtimeErrorResume)
}

func (d *Session) applyFerretEventLocked(event *ferret.DebugEvent, runtimeErrorResume bool) {
	if event == nil {
		d.failLocked(errors.New("debug execution returned no event"))

		return
	}

	d.location = convertLocation(event.Location)
	switch event.Reason {
	case ferret.DebugReasonEntry:
		d.stopLocked(StopEntry)
	case ferret.DebugReasonBreakpoint:
		d.hitBreakpointIDs = make([]uint64, len(event.HitBreakpointIDs))
		for index, id := range event.HitBreakpointIDs {
			d.hitBreakpointIDs[index] = uint64(id)
		}
		d.stopLocked(StopBreakpoint)
	case ferret.DebugReasonStep:
		d.stopLocked(StopStep)
	case ferret.DebugReasonPause:
		d.stopLocked(StopPause)
	case ferret.DebugReasonRuntimeError:
		d.reason = StopRuntimeError
		d.state = StateStopped
		d.failure = d.debugFailure(event.Error)
		d.publishLocked(EventStopped, false)
	case ferret.DebugReasonCompleted:
		d.state = StateCompleted
		d.output = d.runtime.Output(event.Output)
		d.publishLocked(EventCompleted, true)
	case ferret.DebugReasonTerminated:
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

func (d *Session) stopLocked(reason StopReason) {
	d.reason = reason
	d.failure = nil
	d.state = StateStopped
	d.publishLocked(EventStopped, false)
}

func (d *Session) failLocked(err error) {
	d.state = StateFailed
	d.failure = d.debugFailure(err)
	d.publishLocked(EventFailed, true)
}

func (d *Session) debugFailure(err error) *Failure {
	if err == nil {
		return nil
	}

	return d.runtime.Failure(err)
}

func (d *Session) requireInspectable() error {
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

func (d *Session) requireStopped() error {
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

func (d *Session) requestTermination() {
	d.mu.Lock()
	if d.state.Terminal() || d.terminating {
		d.mu.Unlock()

		return
	}

	d.terminating = true
	d.mu.Unlock()

	go d.terminate()
}

func (d *Session) terminate() {
	_ = d.runtime.Close()

	d.mu.Lock()
	if !d.state.Terminal() {
		d.state = StateTerminated
		d.reason = StopNone
		d.location = Location{}
		d.failure = nil
		d.publishLocked(EventTerminated, true)
	}
	d.mu.Unlock()
}

func (d *Session) beginClose() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.close.Begin()
}

func (d *Session) settleClose() {
	d.requestTermination()
	<-d.terminalDone
	_ = d.runtime.Close()

	d.mu.Lock()
	for id, watcher := range d.watchers {
		d.closeWatcherLocked(id, watcher, nil)
	}
	d.mu.Unlock()
}

func (d *Session) completeClose() {
	d.close.Finish(d.runtime.Close())
}

func (d *Session) snapshotLocked() SessionSnapshot {
	return SessionSnapshot{
		ID:               d.id,
		Session:          d.runtime.SessionID(),
		State:            d.state,
		Reason:           d.reason,
		Location:         d.location,
		HitBreakpointIDs: append([]uint64(nil), d.hitBreakpointIDs...),
		Parameters:       d.runtime.Parameters(),
		Options:          d.runtime.Options(),
		Output:           d.output.Clone(),
		Failure:          d.failure.Clone(),
	}
}

func (d *Session) publishLocked(kind EventKind, terminal bool) {
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

func (d *Session) unsubscribe(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	watcher, ok := d.watchers[id]
	if !ok {
		return
	}

	d.closeWatcherLocked(id, watcher, nil)
}

func (d *Session) closeWatcherLocked(id uint64, watcher *debugEventWatcher, err error) {
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
