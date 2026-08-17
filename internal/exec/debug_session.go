package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/diagnostic"
)

type (
	// DebugSession owns one retained Ferret debugger session.
	DebugSession struct {
		mu        sync.Mutex
		controlMu sync.Mutex

		id               DebugSessionID
		session          SessionID
		debugger         *ferret.DebugSession
		sourceURI        string
		sourceText       string
		parameters       map[string]any
		options          DebugSessionOptions
		state            DebugState
		reason           DebugStopReason
		location         DebugLocation
		hitBreakpointIDs []uint64
		output           *Output
		failure          *DebugFailure
		breakpoints      map[string][]ferret.DebugBreakpoint
		terminating      bool
		closing          bool
		terminalDone     chan struct{}
		closeDone        chan struct{}
		ferretClose      sync.Once
		ferretCloseDone  chan struct{}
		ferretCloseErr   error
		sequence         uint64
		lastEvent        DebugEvent
		nextWatcher      uint64
		watchers         map[uint64]*debugEventWatcher
	}

	debugEventWatcher struct {
		events chan DebugEvent
		errors chan error
		closed bool
	}
)

func newDebugSession(
	id DebugSessionID,
	parent *Session,
	debuggerSession *ferret.DebugSession,
	parameters map[string]any,
	options DebugSessionOptions,
) *DebugSession {
	result := &DebugSession{
		id:              id,
		session:         parent.id,
		debugger:        debuggerSession,
		sourceURI:       string(parent.source.URI),
		sourceText:      parent.text,
		parameters:      cloneParameters(parameters),
		options:         options,
		state:           DebugStateCreated,
		breakpoints:     make(map[string][]ferret.DebugBreakpoint),
		terminalDone:    make(chan struct{}),
		closeDone:       make(chan struct{}),
		ferretCloseDone: make(chan struct{}),
		watchers:        make(map[uint64]*debugEventWatcher),
	}
	result.publishLocked(DebugEventCreated, false)

	return result
}

// Snapshot returns an immutable DebugSession view.
func (d *DebugSession) Snapshot() DebugSessionSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

// Start commits RUNNING and asynchronously waits for Ferret's first stop.
func (d *DebugSession) Start(ctx context.Context) (DebugSessionSnapshot, error) {
	return d.startCommand(ctx, DebugStateCreated, func() (*ferret.DebugEvent, error) {
		return d.debugger.Start(context.Background())
	}, false)
}

// Continue resumes execution until Ferret reports its next event.
func (d *DebugSession) Continue(ctx context.Context) (DebugSessionSnapshot, error) {
	return d.resumeCommand(ctx, d.debugger.Continue)
}

// StepIn resumes and stops at the next logical source location.
func (d *DebugSession) StepIn(ctx context.Context) (DebugSessionSnapshot, error) {
	return d.resumeCommand(ctx, d.debugger.Step)
}

// StepOver resumes and stops at the next location in the same or a shallower frame.
func (d *DebugSession) StepOver(ctx context.Context) (DebugSessionSnapshot, error) {
	return d.resumeCommand(ctx, d.debugger.Next)
}

// StepOut resumes and stops in a caller frame.
func (d *DebugSession) StepOut(ctx context.Context) (DebugSessionSnapshot, error) {
	return d.resumeCommand(ctx, d.debugger.Out)
}

// Pause requests a safe stop without waiting for the active command.
func (d *DebugSession) Pause(ctx context.Context) (DebugSessionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return DebugSessionSnapshot{}, err
	}

	d.mu.Lock()
	if d.state != DebugStateRunning {
		d.mu.Unlock()

		return DebugSessionSnapshot{}, ErrDebugSessionNotRunning
	}
	d.mu.Unlock()

	if err := d.debugger.Pause(); err != nil {
		return DebugSessionSnapshot{}, err
	}

	return d.Snapshot(), nil
}

// ReplaceBreakpoints replaces every breakpoint for one source file.
func (d *DebugSession) ReplaceBreakpoints(
	ctx context.Context,
	file string,
	locations []DebugBreakpointLocation,
) ([]DebugBreakpoint, error) {
	if err := contextError(ctx); err != nil {
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
		if err := d.debugger.DeleteBreakpoint(breakpoint.ID); err != nil {
			return nil, err
		}
	}

	d.mu.Lock()
	d.breakpoints[file] = nil
	d.mu.Unlock()

	bound := make([]ferret.DebugBreakpoint, 0, len(locations))
	result := make([]DebugBreakpoint, 0, len(locations))
	for _, location := range locations {
		breakpoint, err := d.debugger.SetBreakpointAt(
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
		result = append(result, convertDebugBreakpoint(breakpoint))
	}

	d.mu.Lock()
	d.breakpoints[file] = bound
	d.mu.Unlock()

	return result, nil
}

// Frames returns the paused frame stack in current-to-caller order.
func (d *DebugSession) Frames(ctx context.Context) ([]DebugFrame, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	frames, err := d.debugger.Frames()
	if err != nil {
		return nil, err
	}

	result := make([]DebugFrame, len(frames))
	for index, frame := range frames {
		result[index] = DebugFrame{
			Index:    index,
			Name:     frame.Name,
			Location: convertDebugLocation(frame.Location),
		}
	}

	return result, nil
}

// Scopes returns Locals and Parameters for one paused frame.
func (d *DebugSession) Scopes(ctx context.Context, frame int) ([]DebugScope, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	variables, err := d.debugger.FrameLocals(frame)
	if err != nil {
		return nil, err
	}

	locals := DebugScope{Kind: DebugScopeLocals, Name: "Locals"}
	parameters := DebugScope{Kind: DebugScopeParameters, Name: "Parameters"}
	for _, variable := range variables {
		converted := convertDebugVariable(variable)

		if variable.Param {
			parameters.Variables = append(parameters.Variables, converted)
		} else {
			locals.Variables = append(locals.Variables, converted)
		}
	}

	return []DebugScope{locals, parameters}, nil
}

// Variables expands one value reference from the current paused state.
func (d *DebugSession) Variables(ctx context.Context, reference DebugValueReference) ([]DebugVariable, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return nil, err
	}

	variables, err := d.debugger.Variables(ferret.DebugValueReference(reference))
	if err != nil {
		return nil, err
	}

	return convertDebugVariables(variables), nil
}

// Evaluate evaluates a side-effect-free expression in one paused frame.
func (d *DebugSession) Evaluate(ctx context.Context, frame int, expression string) (DebugValue, error) {
	if err := contextError(ctx); err != nil {
		return DebugValue{}, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := d.requireStopped(); err != nil {
		return DebugValue{}, err
	}

	value, err := d.debugger.EvaluateFrame(ctx, frame, expression)
	if err != nil {
		return DebugValue{}, err
	}

	return convertDebugValue(value), nil
}

// Terminate idempotently requests Ferret termination while retaining the resource.
func (d *DebugSession) Terminate(ctx context.Context) (DebugSessionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return DebugSessionSnapshot{}, err
	}

	d.requestTermination()

	return d.Snapshot(), nil
}

// Subscribe returns the latest lifecycle event and future bounded observations.
func (d *DebugSession) Subscribe() DebugSubscription {
	d.mu.Lock()
	current := cloneDebugEvent(d.lastEvent)
	if d.state.Terminal() {
		events := make(chan DebugEvent)
		errorsChannel := make(chan error)
		close(events)
		close(errorsChannel)
		d.mu.Unlock()

		return DebugSubscription{Current: current, Events: events, Errors: errorsChannel, Cancel: func() {}}
	}

	d.nextWatcher++
	id := d.nextWatcher
	watcher := &debugEventWatcher{
		events: make(chan DebugEvent, watcherBufferSize),
		errors: make(chan error, 1),
	}
	d.watchers[id] = watcher
	d.mu.Unlock()

	var once sync.Once

	return DebugSubscription{
		Current: current,
		Events:  watcher.events,
		Errors:  watcher.errors,
		Cancel: func() {
			once.Do(func() { d.unsubscribe(id) })
		},
	}
}

func (d *DebugSession) resumeCommand(
	ctx context.Context,
	command func(context.Context) (*ferret.DebugEvent, error),
) (DebugSessionSnapshot, error) {
	d.mu.Lock()
	runtimeErrorResume := d.state == DebugStateStopped && d.reason == DebugStopRuntimeError
	d.mu.Unlock()

	return d.startCommand(ctx, DebugStateStopped, func() (*ferret.DebugEvent, error) {
		return command(context.Background())
	}, runtimeErrorResume)
}

func (d *DebugSession) startCommand(
	ctx context.Context,
	expected DebugState,
	command func() (*ferret.DebugEvent, error),
	runtimeErrorResume bool,
) (DebugSessionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return DebugSessionSnapshot{}, err
	}

	d.controlMu.Lock()
	defer d.controlMu.Unlock()

	if err := contextError(ctx); err != nil {
		return DebugSessionSnapshot{}, err
	}

	d.mu.Lock()
	if d.terminating || d.closing {
		d.mu.Unlock()

		return DebugSessionSnapshot{}, ErrDebugSessionTerminal
	}

	if d.state != expected {
		actual := d.state
		d.mu.Unlock()

		if actual == DebugStateRunning {
			return DebugSessionSnapshot{}, ErrDebugSessionRunning
		}

		return DebugSessionSnapshot{}, ErrDebugSessionTerminal
	}

	d.state = DebugStateRunning
	d.reason = DebugStopNone
	d.location = DebugLocation{}
	d.hitBreakpointIDs = nil
	d.failure = nil
	d.publishLocked(DebugEventRunning, false)
	snapshot := d.snapshotLocked()
	d.mu.Unlock()

	go d.runCommand(command, runtimeErrorResume)

	return snapshot, nil
}

func (d *DebugSession) runCommand(command func() (*ferret.DebugEvent, error), runtimeErrorResume bool) {
	event, err := command()

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state != DebugStateRunning {
		return
	}

	if err != nil {
		if d.terminating {
			d.state = DebugStateTerminated
			d.publishLocked(DebugEventTerminated, true)

			return
		}

		d.failLocked(err)

		return
	}

	d.applyFerretEventLocked(event, runtimeErrorResume)
}

func (d *DebugSession) applyFerretEventLocked(event *ferret.DebugEvent, runtimeErrorResume bool) {
	if event == nil {
		d.failLocked(errors.New("debug execution returned no event"))

		return
	}

	d.location = convertDebugLocation(event.Location)
	switch event.Reason {
	case ferret.DebugReasonEntry:
		d.stopLocked(DebugStopEntry)
	case ferret.DebugReasonBreakpoint:
		d.hitBreakpointIDs = make([]uint64, len(event.HitBreakpointIDs))
		for index, id := range event.HitBreakpointIDs {
			d.hitBreakpointIDs[index] = uint64(id)
		}
		d.stopLocked(DebugStopBreakpoint)
	case ferret.DebugReasonStep:
		d.stopLocked(DebugStopStep)
	case ferret.DebugReasonPause:
		d.stopLocked(DebugStopPause)
	case ferret.DebugReasonRuntimeError:
		d.reason = DebugStopRuntimeError
		d.state = DebugStateStopped
		d.failure = d.debugFailure(event.Error)
		d.publishLocked(DebugEventStopped, false)
	case ferret.DebugReasonCompleted:
		d.state = DebugStateCompleted
		if event.Output != nil {
			d.output = &Output{
				ContentType: event.Output.ContentType,
				Content:     append([]byte(nil), event.Output.Content...),
			}
		}
		d.publishLocked(DebugEventCompleted, true)
	case ferret.DebugReasonTerminated:
		if runtimeErrorResume && event.Error != nil && !d.terminating {
			d.failLocked(event.Error)
		} else {
			d.state = DebugStateTerminated
			d.publishLocked(DebugEventTerminated, true)
		}
	default:
		d.failLocked(errors.New("debug execution returned an unknown event"))
	}
}

func (d *DebugSession) stopLocked(reason DebugStopReason) {
	d.reason = reason
	d.failure = nil
	d.state = DebugStateStopped
	d.publishLocked(DebugEventStopped, false)
}

func (d *DebugSession) failLocked(err error) {
	d.state = DebugStateFailed
	d.failure = d.debugFailure(err)
	d.publishLocked(DebugEventFailed, true)
}

func (d *DebugSession) debugFailure(err error) *DebugFailure {
	if err == nil {
		return nil
	}

	return &DebugFailure{
		Message:     err.Error(),
		Diagnostics: diagnostic.FromError(d.sourceURI, d.sourceText, err),
	}
}

func (d *DebugSession) requireInspectable() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == DebugStateRunning {
		return ErrDebugSessionRunning
	}

	if d.state.Terminal() || d.closing {
		return ErrDebugSessionTerminal
	}

	return nil
}

func (d *DebugSession) requireStopped() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == DebugStateRunning {
		return ErrDebugSessionRunning
	}

	if d.state != DebugStateStopped || d.closing {
		return ErrDebugSessionNotStopped
	}

	return nil
}

func (d *DebugSession) requestTermination() {
	d.mu.Lock()
	if d.state.Terminal() || d.terminating {
		d.mu.Unlock()

		return
	}

	d.terminating = true
	d.mu.Unlock()

	go d.terminate()
}

func (d *DebugSession) terminate() {
	d.closeFerret()

	d.mu.Lock()
	if !d.state.Terminal() {
		d.state = DebugStateTerminated
		d.reason = DebugStopNone
		d.location = DebugLocation{}
		d.failure = nil
		d.publishLocked(DebugEventTerminated, true)
	}
	d.mu.Unlock()
}

func (d *DebugSession) closeFerret() {
	d.ferretClose.Do(func() {
		d.ferretCloseErr = d.debugger.Close()
		close(d.ferretCloseDone)
	})
	<-d.ferretCloseDone
}

func (d *DebugSession) beginClose() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closing {
		return false
	}

	d.closing = true

	return true
}

func (d *DebugSession) settleClose() {
	d.requestTermination()
	<-d.terminalDone
	d.closeFerret()

	d.mu.Lock()
	for id, watcher := range d.watchers {
		d.closeWatcherLocked(id, watcher, nil)
	}
	d.mu.Unlock()
}

func (d *DebugSession) completeClose() {
	close(d.closeDone)
}

func (d *DebugSession) closeResult() error {
	<-d.closeDone

	return d.ferretCloseErr
}

func (d *DebugSession) snapshotLocked() DebugSessionSnapshot {
	return DebugSessionSnapshot{
		ID:               d.id,
		Session:          d.session,
		State:            d.state,
		Reason:           d.reason,
		Location:         d.location,
		HitBreakpointIDs: append([]uint64(nil), d.hitBreakpointIDs...),
		Parameters:       cloneParameters(d.parameters),
		Options:          d.options,
		Output:           cloneOutput(d.output),
		Failure:          cloneDebugFailure(d.failure),
	}
}

func (d *DebugSession) publishLocked(kind DebugEventKind, terminal bool) {
	d.sequence++
	d.lastEvent = DebugEvent{
		DebugSession: d.id,
		Sequence:     d.sequence,
		Kind:         kind,
		Snapshot:     d.snapshotLocked(),
	}

	for id, watcher := range d.watchers {
		select {
		case watcher.events <- cloneDebugEvent(d.lastEvent):
			if terminal {
				d.closeWatcherLocked(id, watcher, nil)
			}
		default:
			d.closeWatcherLocked(id, watcher, ErrDebugWatcherLagged)
		}
	}

	if terminal {
		close(d.terminalDone)
	}
}

func (d *DebugSession) unsubscribe(id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	watcher, ok := d.watchers[id]
	if !ok {
		return
	}

	d.closeWatcherLocked(id, watcher, nil)
}

func (d *DebugSession) closeWatcherLocked(id uint64, watcher *debugEventWatcher, err error) {
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
