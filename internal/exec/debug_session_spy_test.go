package exec

import (
	"context"
	"sync"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

type debugSessionSpy struct {
	runtime *runtimeSpy

	closeOnce sync.Once
	closeErr  error
}

var _ apidebugger.Session = (*debugSessionSpy)(nil)

func (s *debugSessionSpy) Start(context.Context) (*apidebugger.Event, error) {
	return &apidebugger.Event{Reason: apidebugger.ReasonEntry}, nil
}

func (s *debugSessionSpy) Continue(context.Context) (*apidebugger.Event, error) {
	return &apidebugger.Event{Reason: apidebugger.ReasonCompleted}, nil
}

func (s *debugSessionSpy) StepIn(ctx context.Context) (*apidebugger.Event, error) {
	return s.Continue(ctx)
}

func (s *debugSessionSpy) StepOver(ctx context.Context) (*apidebugger.Event, error) {
	return s.Continue(ctx)
}

func (s *debugSessionSpy) StepOut(ctx context.Context) (*apidebugger.Event, error) {
	return s.Continue(ctx)
}

func (s *debugSessionSpy) Pause() error {
	return nil
}

func (s *debugSessionSpy) SetBreakpoint(location apisource.Location) (apidebugger.Breakpoint, error) {
	return s.SetBreakpointAt(location, apidebugger.BreakpointOptions{})
}

func (s *debugSessionSpy) SetBreakpointAt(
	location apisource.Location,
	options apidebugger.BreakpointOptions,
) (apidebugger.Breakpoint, error) {
	return apidebugger.Breakpoint{
		RequestedLocation: location,
		BindingMode:       options.BindingMode,
		Bound:             true,
	}, nil
}

func (s *debugSessionSpy) DeleteBreakpoint(apidebugger.BreakpointID) error {
	return nil
}

func (s *debugSessionSpy) Breakpoints() []apidebugger.Breakpoint {
	return nil
}

func (s *debugSessionSpy) Frames() ([]apidebugger.Frame, error) {
	return nil, nil
}

func (s *debugSessionSpy) Locals() ([]apidebugger.Variable, error) {
	return nil, nil
}

func (s *debugSessionSpy) FrameLocals(int) ([]apidebugger.Variable, error) {
	return nil, nil
}

func (s *debugSessionSpy) Variables(apidebugger.ValueReference) ([]apidebugger.Variable, error) {
	return nil, nil
}

func (s *debugSessionSpy) Evaluate(context.Context, string) (apidebugger.Value, error) {
	return apidebugger.Value{}, nil
}

func (s *debugSessionSpy) EvaluateFrame(context.Context, int, string) (apidebugger.Value, error) {
	return apidebugger.Value{}, nil
}

func (s *debugSessionSpy) Close() error {
	s.closeOnce.Do(func() {
		if s.runtime.sessionClose != nil {
			s.closeErr = s.runtime.sessionClose()
		}
	})

	return s.closeErr
}
