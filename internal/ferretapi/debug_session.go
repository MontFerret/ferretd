package ferretapi

import (
	"context"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferret/v2"
)

type debugSession struct {
	session *ferret.DebugSession
}

var _ apidebugger.Session = (*debugSession)(nil)

func (s *debugSession) Start(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Start(ctx)

	return convertEvent(event), wrapDiagnosticError(err)
}

func (s *debugSession) Continue(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Continue(ctx)

	return convertEvent(event), wrapDiagnosticError(err)
}

func (s *debugSession) StepIn(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.StepIn(ctx)

	return convertEvent(event), wrapDiagnosticError(err)
}

func (s *debugSession) StepOver(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.StepOver(ctx)

	return convertEvent(event), wrapDiagnosticError(err)
}

func (s *debugSession) StepOut(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.StepOut(ctx)

	return convertEvent(event), wrapDiagnosticError(err)
}

func (s *debugSession) Pause() error {
	return wrapDiagnosticError(s.session.Pause())
}

func (s *debugSession) SetBreakpoint(location apisource.Location) (apidebugger.Breakpoint, error) {
	value, err := s.session.SetBreakpoint(location)

	return value, wrapDiagnosticError(err)
}

func (s *debugSession) SetBreakpointAt(location apisource.Location, options apidebugger.BreakpointOptions) (apidebugger.Breakpoint, error) {
	value, err := s.session.SetBreakpointAt(location, options)

	return value, wrapDiagnosticError(err)
}

func (s *debugSession) DeleteBreakpoint(id apidebugger.BreakpointID) error {
	return wrapDiagnosticError(s.session.DeleteBreakpoint(id))
}

func (s *debugSession) Breakpoints() []apidebugger.Breakpoint { return s.session.Breakpoints() }

func (s *debugSession) Frames() ([]apidebugger.Frame, error) {
	values, err := s.session.Frames()

	return values, wrapDiagnosticError(err)
}

func (s *debugSession) Locals() ([]apidebugger.Variable, error) {
	values, err := s.session.Locals()

	return values, wrapDiagnosticError(err)
}

func (s *debugSession) FrameLocals(frame int) ([]apidebugger.Variable, error) {
	values, err := s.session.FrameLocals(frame)

	return values, wrapDiagnosticError(err)
}

func (s *debugSession) Variables(reference apidebugger.ValueReference) ([]apidebugger.Variable, error) {
	values, err := s.session.Variables(reference)

	return values, wrapDiagnosticError(err)
}

func (s *debugSession) Evaluate(ctx context.Context, expression string) (apidebugger.Value, error) {
	value, err := s.session.Evaluate(ctx, expression)

	return value, wrapDiagnosticError(err)
}

func (s *debugSession) EvaluateFrame(
	ctx context.Context,
	frame int,
	expression string,
) (apidebugger.Value, error) {
	value, err := s.session.EvaluateFrame(ctx, frame, expression)

	return value, wrapDiagnosticError(err)
}

func (s *debugSession) Close() error { return s.session.Close() }
