package debug

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestNewRequiresExecutionManager(t *testing.T) {
	manager, err := New(nil)
	if manager != nil {
		t.Fatal("New returned a manager for a nil execution dependency")
	}
	if !errors.Is(err, errNilExecutionManager) {
		t.Fatalf("New error = %v, want %v", err, errNilExecutionManager)
	}
}

func TestManagerDoesNotOwnExecutionManager(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	if fixture.manager.executions != fixture.executions {
		t.Fatal("New did not retain the supplied execution manager")
	}

	if err := fixture.manager.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := fixture.executions.GetSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("execution Session after debug manager Close: %v", err)
	}
	if _, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		nil,
		exec.RuntimeOptions{},
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("CreateSession after Close error = %v, want ErrClosed", err)
	}
}

func TestManagerCloseSettlesMultipleParentGroups(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	ctx := context.Background()
	secondParent, err := fixture.executions.CreateSession(ctx, fixture.workspace.ID(), "query.fql")
	if err != nil {
		t.Fatalf("CreateSession second parent: %v", err)
	}

	parents := []exec.SessionSnapshot{fixture.session, secondParent}
	created := make([]SessionSnapshot, 0, len(parents)*2)
	for _, parent := range parents {
		for range 2 {
			session, err := fixture.manager.CreateSession(ctx, parent.ID, nil, exec.RuntimeOptions{})
			if err != nil {
				t.Fatalf("CreateSession debug child: %v", err)
			}
			created = append(created, session)
		}
	}

	if err := fixture.manager.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, session := range created {
		if _, err := fixture.manager.GetSession(ctx, session.ID); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("GetSession after Close error = %v, want ErrSessionNotFound", err)
		}
	}
	for _, parent := range parents {
		if _, err := fixture.executions.GetSession(ctx, parent.ID); err != nil {
			t.Fatalf("borrowed execution Session after debug Close: %v", err)
		}
	}

	fixture.manager.mu.RLock()
	groups := len(fixture.manager.groups)
	fixture.manager.mu.RUnlock()
	if groups != 0 {
		t.Fatalf("debug parent groups after Close = %d, want 0", groups)
	}
	if err := fixture.manager.Close(ctx); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
}

func TestCreateSessionCopiesParameters(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	input := map[string]any{
		"value":  7,
		"nested": map[string]any{"items": []any{"one", "two"}},
	}

	created, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		input,
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	input["value"] = 99
	input["nested"].(map[string]any)["items"].([]any)[0] = "caller"
	created.Parameters["nested"].(map[string]any)["items"].([]any)[1] = "snapshot"

	stored, err := fixture.manager.GetSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Parameters["value"] != 7 ||
		stored.Parameters["nested"].(map[string]any)["items"].([]any)[0] != "one" ||
		stored.Parameters["nested"].(map[string]any)["items"].([]any)[1] != "two" {
		t.Fatalf("stored parameters = %#v, want immutable copy", stored.Parameters)
	}
}

func TestCreateSessionRejectsInvalidParameters(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")

	if _, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		map[string]any{"invalid": make(chan int)},
		exec.RuntimeOptions{},
	); err == nil {
		t.Fatal("CreateSession unexpectedly accepted invalid parameters")
	}
}

func TestDebugManagerRequiresContexts(t *testing.T) {
	t.Run("operation", func(t *testing.T) {
		fixture := newDebugFixture(t, "RETURN 1")
		assertPanics(t, func() {
			//lint:ignore SA1012 This test verifies that a required context cannot be nil.
			_, _ = fixture.manager.CreateSession(nil, fixture.session.ID, nil, exec.RuntimeOptions{})
		})
	})

	t.Run("close wait", func(t *testing.T) {
		fixture := newDebugFixture(t, "RETURN 1")
		created, err := fixture.manager.CreateSession(
			context.Background(),
			fixture.session.ID,
			nil,
			exec.RuntimeOptions{},
		)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		assertPanics(t, func() {
			//lint:ignore SA1012 This test verifies that a required context cannot be nil.
			_ = fixture.manager.CloseSession(nil, created.ID)
		})
	})
}

func TestDebugSessionLifecycleBreakpointsFramesScopesAndEvaluation(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN @input")
	created, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		map[string]any{"input": 2},
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.State != StateCreated || created.Options.OutputContentType != "application/json" {
		t.Fatalf("created = %+v", created)
	}

	debuggerSession := fixture.runtime.latestDebugger()
	if debuggerSession == nil {
		t.Fatal("debugger session was not created")
	}

	program := filepath.Join(fixture.workspace.Root(), "query.fql")
	var breakpointSequence atomic.Int64
	breakpointSequence.Store(40)
	debuggerSession.setFn = func(
		location apisource.Location,
		options apidebugger.BreakpointOptions,
	) (apidebugger.Breakpoint, error) {
		id := apidebugger.BreakpointID(breakpointSequence.Add(1))

		return apidebugger.Breakpoint{
			Location: apisource.Range{
				Location: apisource.Location{
					SourceName: location.SourceName,
					Position: apisource.Position{
						Line:   location.Line + 1,
						Column: location.Column + 2,
					},
				},
				Span: apisource.Span{Start: 10, End: 20},
			},
			RequestedLocation: location,
			ID:                id,
			PointID:           apidebugger.PointID(id + 100),
			FunctionID:        apidebugger.FunctionID(id + 200),
			BindingMode:       options.BindingMode,
			Bound:             true,
		}, nil
	}

	firstBreakpoints, err := fixture.manager.ReplaceBreakpoints(
		context.Background(),
		created.ID,
		program,
		[]apisource.Position{{Line: 2, Column: 3}},
	)
	if err != nil {
		t.Fatalf("ReplaceBreakpoints first: %v", err)
	}
	if len(firstBreakpoints) != 1 || firstBreakpoints[0].ID != 41 {
		t.Fatalf("first breakpoints = %+v", firstBreakpoints)
	}

	breakpoints, err := fixture.manager.ReplaceBreakpoints(
		context.Background(),
		created.ID,
		program,
		[]apisource.Position{{Line: 3, Column: 4}},
	)
	if err != nil {
		t.Fatalf("ReplaceBreakpoints second: %v", err)
	}
	if len(breakpoints) != 1 || breakpoints[0].ID != 42 || !breakpoints[0].Bound ||
		breakpoints[0].RequestedLocation.SourceName != program ||
		breakpoints[0].RequestedLocation.Position != (apisource.Position{Line: 3, Column: 4}) ||
		breakpoints[0].Location.Position != (apisource.Position{Line: 4, Column: 6}) ||
		breakpoints[0].Location.Span != (apisource.Span{Start: 10, End: 20}) ||
		breakpoints[0].PointID != 142 || breakpoints[0].FunctionID != 242 ||
		breakpoints[0].BindingMode != apidebugger.BreakpointBindNextExecutableInSource {
		t.Fatalf("canonical breakpoint = %+v", breakpoints)
	}

	debuggerSession.frames = []apidebugger.Frame{
		{
			Name:       "callee",
			Location:   apisource.Location{SourceName: program, Position: apisource.Position{Line: 4, Column: 2}},
			FunctionID: 91,
		},
		{
			Name:       "caller",
			Location:   apisource.Location{SourceName: program, Position: apisource.Position{Line: 8, Column: 1}},
			FunctionID: 92,
		},
	}
	debuggerSession.locals[0] = []apidebugger.Variable{
		{Name: "local", Value: apidebugger.Value{Type: "Number", Display: "3", Reference: 77}},
		{Name: "@input", Value: apidebugger.Value{Type: "Number", Display: "2"}, Param: true},
	}
	debuggerSession.variables[77] = []apidebugger.Variable{{
		Name:  "child",
		Value: apidebugger.Value{Type: "Number", Display: "4"},
	}}
	debuggerSession.values[1] = map[string]apidebugger.Value{
		"@input + 3": {Type: "Number", Display: "5", Reference: 88},
	}

	pauseRequested := make(chan struct{})
	var pauseOnce sync.Once
	var continueCalls atomic.Int64
	debuggerSession.startFn = func(context.Context) (*apidebugger.Event, error) {
		return debuggerEvent(apidebugger.ReasonEntry, program, 1), nil
	}
	debuggerSession.continueFn = func(ctx context.Context) (*apidebugger.Event, error) {
		switch continueCalls.Add(1) {
		case 1:
			event := debuggerEvent(apidebugger.ReasonBreakpoint, program, 4)
			event.HitBreakpointIDs = []apidebugger.BreakpointID{breakpoints[0].ID}

			return event, nil
		case 2:
			select {
			case <-pauseRequested:
				return debuggerEvent(apidebugger.ReasonPause, program, 7), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return &apidebugger.Event{
				Reason: apidebugger.ReasonCompleted,
				Output: &api.Output{
					ContentType: "application/json",
					Content:     []byte(`{"result":5}`),
				},
			}, nil
		}
	}
	debuggerSession.stepInFn = func(context.Context) (*apidebugger.Event, error) {
		return debuggerEvent(apidebugger.ReasonStep, program, 5), nil
	}
	debuggerSession.stepOverFn = func(context.Context) (*apidebugger.Event, error) {
		return debuggerEvent(apidebugger.ReasonStep, program, 6), nil
	}
	debuggerSession.stepOutFn = func(context.Context) (*apidebugger.Event, error) {
		return debuggerEvent(apidebugger.ReasonStep, program, 7), nil
	}
	debuggerSession.pauseFn = func() error {
		pauseOnce.Do(func() { close(pauseRequested) })

		return nil
	}

	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	running, err := fixture.manager.StartSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if running.State != StateRunning {
		t.Fatalf("start state = %v", running.State)
	}
	entry := waitForState(t, subscription, StateStopped)
	if entry.Reason != apidebugger.ReasonEntry || entry.Location.SourceName != program {
		t.Fatalf("entry = %+v", entry)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	stopped := waitForState(t, subscription, StateStopped)
	if stopped.Reason != apidebugger.ReasonBreakpoint || stopped.Location.Line != 4 ||
		len(stopped.HitBreakpointIDs) != 1 || stopped.HitBreakpointIDs[0] != breakpoints[0].ID {
		t.Fatalf("breakpoint stop = %+v", stopped)
	}

	frames, err := fixture.manager.Frames(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	if len(frames) != 2 || frames[0].Name != "callee" || frames[0].FunctionID != 91 ||
		frames[1].Name != "caller" || frames[1].FunctionID != 92 {
		t.Fatalf("frames = %+v", frames)
	}

	scopes, err := fixture.manager.Scopes(context.Background(), created.ID, 0)
	if err != nil {
		t.Fatalf("Scopes: %v", err)
	}
	if len(scopes) != 2 || scopes[0].Kind != ScopeLocals || scopes[1].Kind != ScopeParameters ||
		!debugScopeHas(scopes[0], "local", "3") || !debugScopeHas(scopes[1], "@input", "2") {
		t.Fatalf("scopes = %+v", scopes)
	}
	variables, err := fixture.manager.Variables(context.Background(), created.ID, 77)
	if err != nil || len(variables) != 1 || variables[0].Name != "child" {
		t.Fatalf("variables = %+v, %v", variables, err)
	}

	value, err := fixture.manager.Evaluate(context.Background(), created.ID, 1, "@input + 3")
	if err != nil || value.Display != "5" || value.Reference != 88 {
		t.Fatalf("caller evaluation = %+v, %v", value, err)
	}

	if _, err := fixture.manager.StepInSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StepInSession: %v", err)
	}
	if stepped := waitForState(t, subscription, StateStopped); stepped.Reason != apidebugger.ReasonStep ||
		stepped.Location.Line != 5 {
		t.Fatalf("step-in stop = %+v", stepped)
	}

	if _, err := fixture.manager.StepOverSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StepOverSession: %v", err)
	}
	stepped := waitForState(t, subscription, StateStopped)
	if stepped.Reason != apidebugger.ReasonStep || stepped.Location.Line != 6 {
		t.Fatalf("step stop = %+v", stepped)
	}
	if _, err := fixture.manager.StepOutSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StepOutSession: %v", err)
	}
	if stepped := waitForState(t, subscription, StateStopped); stepped.Reason != apidebugger.ReasonStep ||
		stepped.Location.Line != 7 {
		t.Fatalf("step-out stop = %+v", stepped)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("pause ContinueSession: %v", err)
	}
	if _, err := fixture.manager.PauseSession(context.Background(), created.ID); err != nil {
		t.Fatalf("PauseSession: %v", err)
	}
	paused := waitForState(t, subscription, StateStopped)
	if paused.Reason != apidebugger.ReasonPause || paused.Location.Line != 7 {
		t.Fatalf("pause stop = %+v", paused)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("final ContinueSession: %v", err)
	}
	completed := waitForState(t, subscription, StateCompleted)
	if completed.Output == nil || completed.Output.ContentType != "application/json" {
		t.Fatalf("completed = %+v", completed)
	}
	if _, err := fixture.manager.Frames(context.Background(), created.ID); !errors.Is(err, ErrSessionNotStopped) {
		t.Fatalf("terminal Frames error = %v", err)
	}

	commands := debuggerSession.recordedCommands()
	if !debuggerCommandsInclude(commands, "start", "continue", "pause", "step in", "step over", "step out",
		"set breakpoint", "delete breakpoint", "frames", "frame locals", "variables", "evaluate frame") {
		t.Fatalf("debugger commands = %+v", commands)
	}
}

func TestReplaceBreakpointsRetainsCanonicalPartialReplacement(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(
		context.Background(),
		fixture.session.ID,
		nil,
		exec.RuntimeOptions{},
	)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	debuggerSession := fixture.runtime.latestDebugger()
	if debuggerSession == nil {
		t.Fatal("debugger session was not created")
	}

	program := filepath.Join(fixture.workspace.Root(), "query.fql")
	wantErr := errors.New("bind breakpoint")
	debuggerSession.setFn = func(
		location apisource.Location,
		options apidebugger.BreakpointOptions,
	) (apidebugger.Breakpoint, error) {
		if location.Line == 4 {
			return apidebugger.Breakpoint{}, wantErr
		}

		return apidebugger.Breakpoint{
			ID:                apidebugger.BreakpointID(location.Line),
			RequestedLocation: location,
			Location:          apisource.Range{Location: location},
			BindingMode:       options.BindingMode,
			Bound:             true,
		}, nil
	}

	if _, err := fixture.manager.ReplaceBreakpoints(
		context.Background(),
		created.ID,
		program,
		[]apisource.Position{{Line: 2}},
	); err != nil {
		t.Fatalf("initial ReplaceBreakpoints: %v", err)
	}

	_, err = fixture.manager.ReplaceBreakpoints(
		context.Background(),
		created.ID,
		program,
		[]apisource.Position{{Line: 3}, {Line: 4}},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("partial ReplaceBreakpoints error = %v, want %v", err, wantErr)
	}

	fixture.manager.mu.RLock()
	session := fixture.manager.sessions[created.ID]
	fixture.manager.mu.RUnlock()
	session.mu.Lock()
	retained := append([]apidebugger.Breakpoint(nil), session.breakpoints[program]...)
	session.mu.Unlock()
	if len(retained) != 1 || retained[0].ID != 3 || retained[0].RequestedLocation.SourceName != program {
		t.Fatalf("retained partial breakpoints = %+v", retained)
	}

	if _, err := fixture.manager.ReplaceBreakpoints(
		context.Background(),
		created.ID,
		program,
		nil,
	); err != nil {
		t.Fatalf("clear partial breakpoints: %v", err)
	}

	commands := debuggerSession.recordedCommands()
	deleted := make([]apidebugger.BreakpointID, 0, 2)
	for _, command := range commands {
		if command.name == "delete breakpoint" {
			deleted = append(deleted, command.breakpoint)
		}
	}
	if len(deleted) != 2 || deleted[0] != 2 || deleted[1] != 3 {
		t.Fatalf("deleted breakpoint IDs = %v, want [2 3]", deleted)
	}
}

func TestDebugSessionRuntimeErrorRemainsInspectableThenFailsOnResume(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	debuggerSession := fixture.runtime.latestDebugger()
	if debuggerSession == nil {
		t.Fatal("debugger session was not created")
	}

	program := filepath.Join(fixture.workspace.Root(), "query.fql")
	wantRuntimeError := errors.New("division by zero")
	var continueCalls atomic.Int64
	debuggerSession.startFn = func(context.Context) (*apidebugger.Event, error) {
		return debuggerEvent(apidebugger.ReasonEntry, program, 1), nil
	}
	debuggerSession.continueFn = func(context.Context) (*apidebugger.Event, error) {
		if continueCalls.Add(1) == 1 {
			event := debuggerEvent(apidebugger.ReasonRuntimeError, program, 2)
			event.Error = wantRuntimeError

			return event, nil
		}

		return &apidebugger.Event{
			Reason: apidebugger.ReasonTerminated,
			Error:  wantRuntimeError,
		}, nil
	}
	debuggerSession.locals[0] = []apidebugger.Variable{{
		Name:  "x",
		Value: apidebugger.Value{Type: "Number", Display: "7"},
	}}

	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	runtimeError := waitForState(t, subscription, StateStopped)
	if runtimeError.Reason != apidebugger.ReasonRuntimeError || runtimeError.Failure == nil ||
		runtimeError.Failure.Message != wantRuntimeError.Error() {
		t.Fatalf("runtime error = %+v", runtimeError)
	}

	scopes, err := fixture.manager.Scopes(context.Background(), created.ID, 0)
	if err != nil || !debugScopeHas(scopes[0], "x", "7") {
		t.Fatalf("runtime-error scopes = %+v, %v", scopes, err)
	}

	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("resume runtime error: %v", err)
	}
	failed := waitForState(t, subscription, StateFailed)
	if failed.Failure == nil || failed.Failure.Message == "" {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestDebugSessionTerminateCloseAndParentCascade(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	blockDebuggerOnContinue(t, fixture.runtime.latestDebugger())
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}
	if _, err := fixture.manager.TerminateSession(context.Background(), created.ID); err != nil {
		t.Fatalf("TerminateSession: %v", err)
	}
	terminated := waitForState(t, subscription, StateTerminated)
	if terminated.State != StateTerminated {
		t.Fatalf("terminated = %+v", terminated)
	}

	if err := fixture.executions.CloseSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
	if err := fixture.manager.CloseSession(context.Background(), created.ID); err != nil {
		t.Fatalf("idempotent CloseSession: %v", err)
	}
}

func TestDebugSessionCloseRacesTermination(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	blockDebuggerOnContinue(t, fixture.runtime.latestDebugger())
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}

	start := make(chan struct{})
	terminateDone := make(chan error, 1)
	closeDone := make(chan error, 1)
	go func() {
		<-start
		_, err := fixture.manager.TerminateSession(context.Background(), created.ID)
		terminateDone <- err
	}()
	go func() {
		<-start
		closeDone <- fixture.manager.CloseSession(context.Background(), created.ID)
	}()
	close(start)

	if err := <-terminateDone; err != nil && !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("TerminateSession: %v", err)
	}
	if err := <-closeDone; err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
}

func TestSessionCloseCascadesRunningDebugSession(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	blockDebuggerOnContinue(t, fixture.runtime.latestDebugger())
	subscription, err := fixture.manager.WatchSession(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("WatchSession: %v", err)
	}
	defer subscription.Cancel()

	if _, err := fixture.manager.StartSession(context.Background(), created.ID); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	waitForState(t, subscription, StateStopped)
	if _, err := fixture.manager.ContinueSession(context.Background(), created.ID); err != nil {
		t.Fatalf("ContinueSession: %v", err)
	}

	if err := fixture.executions.CloseSession(context.Background(), fixture.session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := fixture.executions.GetSession(context.Background(), fixture.session.ID); !errors.Is(err, exec.ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
	if _, err := fixture.manager.GetSession(context.Background(), created.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetSession error = %v", err)
	}
}

func TestParentCloseWaitsForInFlightDebugCreation(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	parentID := fixture.session.ID
	if err := fixture.manager.beginCreate(parentID); err != nil {
		t.Fatalf("beginCreate: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.manager.closeExecutionSession(ctx, parentID); !errors.Is(err, context.Canceled) {
		t.Fatalf("closeExecutionSession error = %v, want context.Canceled", err)
	}
	if err := fixture.manager.beginCreate(parentID); !errors.Is(err, exec.ErrSessionClosed) {
		t.Fatalf("beginCreate during parent close error = %v, want exec.ErrSessionClosed", err)
	}

	fixture.manager.mu.RLock()
	group := fixture.manager.groups[parentID]
	fixture.manager.mu.RUnlock()
	if err := group.gate.WaitClose(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent debug cleanup while creation was in flight = %v, want context.Canceled", err)
	}

	fixture.manager.finishCreate(parentID)
	if err := fixture.manager.closeExecutionSession(context.Background(), parentID); err != nil {
		t.Fatalf("wait for parent debug cleanup: %v", err)
	}

	fixture.manager.mu.RLock()
	_, retained := fixture.manager.groups[parentID]
	fixture.manager.mu.RUnlock()
	if retained {
		t.Fatal("parent debug group retained after cleanup")
	}
}

func TestDebugWatcherOverflowDisconnectsLaggingWatcher(t *testing.T) {
	fixture := newDebugFixture(t, "RETURN 1")
	created, err := fixture.manager.CreateSession(context.Background(), fixture.session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fixture.manager.mu.RLock()
	debugSession := fixture.manager.sessions[created.ID]
	fixture.manager.mu.RUnlock()
	lagging := debugSession.subscribe()
	independent := debugSession.subscribe()
	defer independent.Cancel()

	for range watcherBufferSize + 1 {
		debugSession.mu.Lock()
		debugSession.publishLocked(EventRunning, false)
		debugSession.mu.Unlock()
		<-independent.Events
	}

	var got error
	for watchErr := range lagging.Errors {
		got = watchErr
	}
	if !errors.Is(got, ErrWatcherLagged) {
		t.Fatalf("lag error = %v", got)
	}
}

func waitForState(t *testing.T, subscription Subscription, state State) SessionSnapshot {
	t.Helper()

	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatalf("debug events closed before state %d", state)
			}
			if event.Snapshot.State == state {
				return event.Snapshot
			}
		case err := <-subscription.Errors:
			if err != nil {
				t.Fatalf("debug watch error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for debug state %d", state)
		}
	}
}

func debugScopeHas(scope Scope, name, display string) bool {
	for _, variable := range scope.Variables {
		if variable.Name == name && variable.Value.Display == display {
			return true
		}
	}

	return false
}
