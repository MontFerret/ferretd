package debug

import (
	"context"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

type (
	debuggerCommand struct {
		name       string
		location   apisource.Location
		options    apidebugger.BreakpointOptions
		breakpoint apidebugger.BreakpointID
		frame      int
		reference  apidebugger.ValueReference
		expression string
	}

	debuggerSessionSpy struct {
		mu sync.Mutex

		commands    []debuggerCommand
		breakpoints []apidebugger.Breakpoint
		frames      []apidebugger.Frame
		locals      map[int][]apidebugger.Variable
		variables   map[apidebugger.ValueReference][]apidebugger.Variable
		values      map[int]map[string]apidebugger.Value

		startFn    func(context.Context) (*apidebugger.Event, error)
		continueFn func(context.Context) (*apidebugger.Event, error)
		stepInFn   func(context.Context) (*apidebugger.Event, error)
		stepOverFn func(context.Context) (*apidebugger.Event, error)
		stepOutFn  func(context.Context) (*apidebugger.Event, error)
		pauseFn    func() error
		setFn      func(apisource.Location, apidebugger.BreakpointOptions) (apidebugger.Breakpoint, error)
		closeFn    func() error

		closeOnce sync.Once
		closeErr  error
	}
)

var _ apidebugger.Session = (*debuggerSessionSpy)(nil)

func newDebuggerSessionSpy() *debuggerSessionSpy {
	return &debuggerSessionSpy{
		locals:    make(map[int][]apidebugger.Variable),
		variables: make(map[apidebugger.ValueReference][]apidebugger.Variable),
		values:    make(map[int]map[string]apidebugger.Value),
	}
}

func (s *debuggerSessionSpy) Start(ctx context.Context) (*apidebugger.Event, error) {
	s.record(debuggerCommand{name: "start"})
	if s.startFn != nil {
		return s.startFn(ctx)
	}

	return &apidebugger.Event{Reason: apidebugger.ReasonEntry}, nil
}

func (s *debuggerSessionSpy) Continue(ctx context.Context) (*apidebugger.Event, error) {
	s.record(debuggerCommand{name: "continue"})
	if s.continueFn != nil {
		return s.continueFn(ctx)
	}

	return &apidebugger.Event{
		Reason: apidebugger.ReasonCompleted,
		Output: &api.Output{ContentType: "application/json", Content: []byte("1")},
	}, nil
}

func (s *debuggerSessionSpy) StepIn(ctx context.Context) (*apidebugger.Event, error) {
	s.record(debuggerCommand{name: "step in"})
	if s.stepInFn != nil {
		return s.stepInFn(ctx)
	}

	return &apidebugger.Event{Reason: apidebugger.ReasonStep}, nil
}

func (s *debuggerSessionSpy) StepOver(ctx context.Context) (*apidebugger.Event, error) {
	s.record(debuggerCommand{name: "step over"})
	if s.stepOverFn != nil {
		return s.stepOverFn(ctx)
	}

	return &apidebugger.Event{Reason: apidebugger.ReasonStep}, nil
}

func (s *debuggerSessionSpy) StepOut(ctx context.Context) (*apidebugger.Event, error) {
	s.record(debuggerCommand{name: "step out"})
	if s.stepOutFn != nil {
		return s.stepOutFn(ctx)
	}

	return &apidebugger.Event{Reason: apidebugger.ReasonStep}, nil
}

func (s *debuggerSessionSpy) Pause() error {
	s.record(debuggerCommand{name: "pause"})
	if s.pauseFn != nil {
		return s.pauseFn()
	}

	return nil
}

func (s *debuggerSessionSpy) SetBreakpoint(
	location apisource.Location,
) (apidebugger.Breakpoint, error) {
	return s.SetBreakpointAt(location, apidebugger.BreakpointOptions{})
}

func (s *debuggerSessionSpy) SetBreakpointAt(
	location apisource.Location,
	options apidebugger.BreakpointOptions,
) (apidebugger.Breakpoint, error) {
	s.record(debuggerCommand{name: "set breakpoint", location: location, options: options})
	if s.setFn != nil {
		breakpoint, err := s.setFn(location, options)
		if err != nil {
			return apidebugger.Breakpoint{}, err
		}

		s.mu.Lock()
		s.breakpoints = append(s.breakpoints, breakpoint)
		s.mu.Unlock()

		return breakpoint, nil
	}

	s.mu.Lock()
	breakpoint := apidebugger.Breakpoint{
		ID:                apidebugger.BreakpointID(len(s.breakpoints) + 1),
		RequestedLocation: location,
		Location: apisource.Range{
			Location: location,
		},
		BindingMode: options.BindingMode,
		Bound:       true,
	}
	s.breakpoints = append(s.breakpoints, breakpoint)
	s.mu.Unlock()

	return breakpoint, nil
}

func (s *debuggerSessionSpy) DeleteBreakpoint(id apidebugger.BreakpointID) error {
	s.record(debuggerCommand{name: "delete breakpoint", breakpoint: id})

	s.mu.Lock()
	defer s.mu.Unlock()

	for index, breakpoint := range s.breakpoints {
		if breakpoint.ID == id {
			s.breakpoints = append(s.breakpoints[:index], s.breakpoints[index+1:]...)

			break
		}
	}

	return nil
}

func (s *debuggerSessionSpy) Breakpoints() []apidebugger.Breakpoint {
	s.record(debuggerCommand{name: "breakpoints"})

	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]apidebugger.Breakpoint(nil), s.breakpoints...)
}

func (s *debuggerSessionSpy) Frames() ([]apidebugger.Frame, error) {
	s.record(debuggerCommand{name: "frames"})

	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]apidebugger.Frame(nil), s.frames...), nil
}

func (s *debuggerSessionSpy) Locals() ([]apidebugger.Variable, error) {
	s.record(debuggerCommand{name: "locals"})

	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]apidebugger.Variable(nil), s.locals[0]...), nil
}

func (s *debuggerSessionSpy) FrameLocals(frame int) ([]apidebugger.Variable, error) {
	s.record(debuggerCommand{name: "frame locals", frame: frame})

	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]apidebugger.Variable(nil), s.locals[frame]...), nil
}

func (s *debuggerSessionSpy) Variables(
	reference apidebugger.ValueReference,
) ([]apidebugger.Variable, error) {
	s.record(debuggerCommand{name: "variables", reference: reference})

	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]apidebugger.Variable(nil), s.variables[reference]...), nil
}

func (s *debuggerSessionSpy) Evaluate(
	ctx context.Context,
	expression string,
) (apidebugger.Value, error) {
	return s.EvaluateFrame(ctx, 0, expression)
}

func (s *debuggerSessionSpy) EvaluateFrame(
	ctx context.Context,
	frame int,
	expression string,
) (apidebugger.Value, error) {
	s.record(debuggerCommand{name: "evaluate frame", frame: frame, expression: expression})
	if err := ctx.Err(); err != nil {
		return apidebugger.Value{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.values[frame][expression], nil
}

func (s *debuggerSessionSpy) Close() error {
	s.closeOnce.Do(func() {
		s.record(debuggerCommand{name: "close"})
		if s.closeFn != nil {
			s.closeErr = s.closeFn()
		}
	})

	return s.closeErr
}

func (s *debuggerSessionSpy) record(command debuggerCommand) {
	s.mu.Lock()
	s.commands = append(s.commands, command)
	s.mu.Unlock()
}

func (s *debuggerSessionSpy) recordedCommands() []debuggerCommand {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]debuggerCommand(nil), s.commands...)
}
