package exec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

type (
	runtimeSpyOption func(*runtimeSpy)

	runtimeSpy struct {
		mu sync.Mutex

		beforeCompile       func(context.Context) error
		beforeRun           func(context.Context) (context.Context, error)
		sessionClose        func() error
		planClose           func() error
		compileErr          error
		compilePlan         api.Plan
		compileSources      []api.Source
		compileDebugSources []api.Source

		compileCalls      atomic.Int64
		compileDebugCalls atomic.Int64
		closeCalls        atomic.Int64
	}

	planSpy struct {
		runtime *runtimeSpy
		source  api.Source
		debug   bool

		mu           sync.Mutex
		closed       bool
		closeOnce    sync.Once
		closeErr     error
		lastOptions  sessionOptionsSpy
		newSessionFn func(context.Context, ...api.SessionOption) (api.Session, error)
		newDebugFn   func(context.Context, ...api.SessionOption) (apidebugger.Session, error)
	}

	sessionSpy struct {
		runtime *runtimeSpy
		source  api.Source
		options sessionOptionsSpy

		closeOnce sync.Once
		closeErr  error
	}

	debugSessionSpy struct {
		runtime *runtimeSpy

		closeOnce sync.Once
		closeErr  error
	}

	sessionOptionsSpy struct {
		params      map[string]any
		contentType string
		fsRoot      string
	}
)

var (
	_ api.Runtime         = (*runtimeSpy)(nil)
	_ api.Plan            = (*planSpy)(nil)
	_ api.Session         = (*sessionSpy)(nil)
	_ apidebugger.Session = (*debugSessionSpy)(nil)
)

func newRuntimeSpy(options ...runtimeSpyOption) *runtimeSpy {
	result := &runtimeSpy{}
	for _, option := range options {
		option(result)
	}

	return result
}

func withBeforeCompileHook(hook func(context.Context) error) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.beforeCompile = hook
	}
}

func withBeforeRunHook(hook func(context.Context) (context.Context, error)) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.beforeRun = hook
	}
}

func withSessionCloseHook(hook func() error) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.sessionClose = hook
	}
}

func withPlanCloseHook(hook func() error) runtimeSpyOption {
	return func(runtime *runtimeSpy) {
		runtime.planClose = hook
	}
}

func (r *runtimeSpy) Run(
	ctx context.Context,
	source api.Source,
	options ...api.SessionOption,
) (api.Output, error) {
	plan, err := r.Compile(ctx, source)
	if err != nil {
		return api.Output{}, err
	}
	defer func() { _ = plan.Close() }()

	session, err := plan.NewSession(ctx, options...)
	if err != nil {
		return api.Output{}, err
	}
	defer func() { _ = session.Close() }()

	return session.Run(ctx)
}

func (r *runtimeSpy) Compile(
	ctx context.Context,
	source api.Source,
	_ ...api.PlanOption,
) (api.Plan, error) {
	r.compileCalls.Add(1)
	r.mu.Lock()
	r.compileSources = append(r.compileSources, source)
	compileErr := r.compileErr
	compilePlan := r.compilePlan
	r.mu.Unlock()
	if r.beforeCompile != nil {
		if err := r.beforeCompile(ctx); err != nil {
			return nil, err
		}
	}
	if compileErr != nil {
		return compilePlan, compileErr
	}

	return &planSpy{runtime: r, source: source}, nil
}

func (r *runtimeSpy) CompileDebug(
	ctx context.Context,
	source api.Source,
	_ ...api.PlanOption,
) (api.Plan, error) {
	r.compileDebugCalls.Add(1)
	r.mu.Lock()
	r.compileDebugSources = append(r.compileDebugSources, source)
	r.mu.Unlock()
	if r.beforeCompile != nil {
		if err := r.beforeCompile(ctx); err != nil {
			return nil, err
		}
	}

	return &planSpy{runtime: r, source: source, debug: true}, nil
}

func (r *runtimeSpy) Close() error {
	r.closeCalls.Add(1)

	return nil
}

func (r *runtimeSpy) sources() ([]api.Source, []api.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]api.Source(nil), r.compileSources...),
		append([]api.Source(nil), r.compileDebugSources...)
}

func (p *planSpy) Params() []string {
	if strings.Contains(p.source.Content, "@value") {
		return []string{"value"}
	}

	return nil
}

func (p *planSpy) NewSession(
	ctx context.Context,
	options ...api.SessionOption,
) (api.Session, error) {
	if p.newSessionFn != nil {
		return p.newSessionFn(ctx, options...)
	}

	parsed, err := newSessionOptionsSpy(options)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("plan is closed")
	}
	p.lastOptions = parsed

	return &sessionSpy{runtime: p.runtime, source: p.source, options: parsed}, nil
}

func (p *planSpy) NewDebugSession(
	ctx context.Context,
	options ...api.SessionOption,
) (apidebugger.Session, error) {
	if p.newDebugFn != nil {
		return p.newDebugFn(ctx, options...)
	}

	parsed, err := newSessionOptionsSpy(options)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("plan is closed")
	}
	p.lastOptions = parsed

	return &debugSessionSpy{runtime: p.runtime}, nil
}

func (p *planSpy) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()

		if p.runtime.planClose != nil {
			p.closeErr = p.runtime.planClose()
		}
	})

	return p.closeErr
}

func (s *sessionSpy) Run(ctx context.Context) (api.Output, error) {
	if s.runtime.beforeRun != nil {
		var err error
		ctx, err = s.runtime.beforeRun(ctx)
		if err != nil {
			return api.Output{}, err
		}
	}

	if strings.Contains(s.source.Content, "WAITFOR FALSE") {
		<-ctx.Done()

		return api.Output{}, ctx.Err()
	}

	content := []byte("1")
	if strings.Contains(s.source.Content, "@value") {
		encoded, err := json.Marshal(s.options.params["value"])
		if err != nil {
			return api.Output{}, err
		}
		content = encoded
	}

	return api.Output{ContentType: s.options.contentType, Content: content}, nil
}

func (s *sessionSpy) Close() error {
	s.closeOnce.Do(func() {
		if s.runtime.sessionClose != nil {
			s.closeErr = s.runtime.sessionClose()
		}
	})

	return s.closeErr
}

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

func newSessionOptionsSpy(options []api.SessionOption) (sessionOptionsSpy, error) {
	result := sessionOptionsSpy{params: make(map[string]any)}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(&result); err != nil {
			return sessionOptionsSpy{}, err
		}
	}

	return result, nil
}

func (o *sessionOptionsSpy) SetParam(name string, value any) error {
	if _, err := json.Marshal(value); err != nil {
		return err
	}
	o.params[name] = value

	return nil
}

func (o *sessionOptionsSpy) SetParams(parameters map[string]any) error {
	for name, value := range parameters {
		if err := o.SetParam(name, value); err != nil {
			return err
		}
	}

	return nil
}

func (o *sessionOptionsSpy) SetOutputContentType(contentType string) error {
	o.contentType = contentType

	return nil
}

func (o *sessionOptionsSpy) SetFSRoot(root string) error {
	o.fsRoot = root

	return nil
}
