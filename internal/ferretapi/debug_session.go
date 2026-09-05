package ferretapi

import (
	"context"
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apiresult "github.com/MontFerret/api/result"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferret/v2"
)

type debugSession struct {
	session *ferret.DebugSession
	source  api.Source

	closeOnce sync.Once
	closeErr  error
}

var _ apidebugger.Session = (*debugSession)(nil)

func newDebugSession(session *ferret.DebugSession, source api.Source) *debugSession {
	return &debugSession{session: session, source: source}
}

func (s *debugSession) Start(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Start(ctx)

	return s.convertEvent(event), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) Continue(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Continue(ctx)

	return s.convertEvent(event), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) StepIn(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Step(ctx)

	return s.convertEvent(event), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) StepOver(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Next(ctx)

	return s.convertEvent(event), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) StepOut(ctx context.Context) (*apidebugger.Event, error) {
	event, err := s.session.Out(ctx)

	return s.convertEvent(event), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) Pause() error {
	return wrapDiagnosticError(s.source, s.session.Pause())
}

func (s *debugSession) SetBreakpoint(location apisource.Location) (apidebugger.Breakpoint, error) {
	return s.SetBreakpointAt(location, apidebugger.BreakpointOptions{
		BindingMode: apidebugger.BreakpointBindNextExecutableInSource,
	})
}

func (s *debugSession) SetBreakpointAt(
	location apisource.Location,
	options apidebugger.BreakpointOptions,
) (apidebugger.Breakpoint, error) {
	mode, err := nativeBreakpointMode(options.BindingMode)
	if err != nil {
		return apidebugger.Breakpoint{}, err
	}

	breakpoint, err := s.session.SetBreakpointAt(
		ferret.DebugSourceLocation{
			File: location.SourceName,
			Position: ferret.Position{
				Line:   location.Line,
				Column: location.Column,
			},
		},
		ferret.DebugBreakpointOptions{BindingMode: mode},
	)

	return convertBreakpoint(breakpoint), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) DeleteBreakpoint(id apidebugger.BreakpointID) error {
	return wrapDiagnosticError(s.source, s.session.DeleteBreakpoint(ferret.DebugBreakpointID(id)))
}

func (s *debugSession) Breakpoints() []apidebugger.Breakpoint {
	values := s.session.Breakpoints()
	result := make([]apidebugger.Breakpoint, len(values))
	for index := range values {
		result[index] = convertBreakpoint(values[index])
	}

	return result
}

func (s *debugSession) Frames() ([]apidebugger.Frame, error) {
	values, err := s.session.Frames()
	if err != nil {
		return nil, wrapDiagnosticError(s.source, err)
	}

	result := make([]apidebugger.Frame, len(values))
	for index := range values {
		result[index] = apidebugger.Frame{
			Name:       values[index].Name,
			Location:   convertLocation(values[index].Location),
			FunctionID: apidebugger.FunctionID(values[index].FunctionID),
		}
	}

	return result, nil
}

func (s *debugSession) Locals() ([]apidebugger.Variable, error) {
	values, err := s.session.Locals()
	if err != nil {
		return nil, wrapDiagnosticError(s.source, err)
	}

	return convertVariables(values), nil
}

func (s *debugSession) FrameLocals(frame int) ([]apidebugger.Variable, error) {
	values, err := s.session.FrameLocals(frame)
	if err != nil {
		return nil, wrapDiagnosticError(s.source, err)
	}

	return convertVariables(values), nil
}

func (s *debugSession) Variables(
	reference apidebugger.ValueReference,
) ([]apidebugger.Variable, error) {
	values, err := s.session.Variables(ferret.DebugValueReference(reference))
	if err != nil {
		return nil, wrapDiagnosticError(s.source, err)
	}

	return convertVariables(values), nil
}

func (s *debugSession) Evaluate(ctx context.Context, expression string) (apidebugger.Value, error) {
	value, err := s.session.Evaluate(ctx, expression)

	return convertValue(value), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) EvaluateFrame(
	ctx context.Context,
	frame int,
	expression string,
) (apidebugger.Value, error) {
	value, err := s.session.EvaluateFrame(ctx, frame, expression)

	return convertValue(value), wrapDiagnosticError(s.source, err)
}

func (s *debugSession) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.session.Close()
	})

	return s.closeErr
}

func (s *debugSession) convertEvent(value *ferret.DebugEvent) *apidebugger.Event {
	if value == nil {
		return nil
	}

	result := &apidebugger.Event{
		Error:            wrapDiagnosticError(s.source, value.Error),
		Reason:           apidebugger.ReasonFromString(string(value.Reason)),
		HitBreakpointIDs: make([]apidebugger.BreakpointID, len(value.HitBreakpointIDs)),
		Location:         convertRange(value.Location),
		Depth:            value.Depth,
	}
	for index := range value.HitBreakpointIDs {
		result.HitBreakpointIDs[index] = apidebugger.BreakpointID(value.HitBreakpointIDs[index])
	}

	if value.Output != nil {
		result.Output = &apiresult.Output{
			ContentType: value.Output.ContentType,
			Content:     append([]byte(nil), value.Output.Content...),
		}
	}

	return result
}
