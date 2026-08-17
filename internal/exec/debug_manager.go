package exec

import (
	"context"
	"errors"
	"strings"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// CreateDebugSession creates one retained debugger child of a compiled Session.
func (m *Manager) CreateDebugSession(
	ctx context.Context,
	sessionID SessionID,
	parameters map[string]any,
	options DebugSessionOptions,
) (DebugSessionSnapshot, error) {
	if err := contextError(ctx); err != nil {
		return DebugSessionSnapshot{}, err
	}

	params, err := runtime.NewParamsFrom(parameters)
	if err != nil {
		return DebugSessionSnapshot{}, invalidParametersError(err)
	}

	id, err := newDebugSessionID()
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	options.OutputContentType = strings.TrimSpace(options.OutputContentType)
	if options.OutputContentType == "" {
		options.OutputContentType = "application/json"
	}

	m.mu.RLock()
	parent := m.sessions[sessionID]
	closed := m.closed
	m.mu.RUnlock()

	if closed {
		return DebugSessionSnapshot{}, ErrManagerClosed
	}

	if parent == nil {
		return DebugSessionSnapshot{}, ErrSessionNotFound
	}

	if err := parent.beginDebugCreate(); err != nil {
		return DebugSessionSnapshot{}, err
	}
	defer parent.finishDebugCreate()

	plan, err := parent.debugCompilation(ctx)
	if err != nil {
		return DebugSessionSnapshot{}, m.debugCompilationError(parent, err)
	}

	sessionOptions := []ferret.SessionOption{ferret.WithSessionRuntimeParams(params)}
	if options.OutputContentType != "" {
		sessionOptions = append(sessionOptions, ferret.WithOutputContentType(options.OutputContentType))
	}

	ferretSession, err := plan.NewDebugSession(ctx, sessionOptions...)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	created := newDebugSession(id, parent, ferretSession, parameters, options)
	if err := parent.addDebugSession(created); err != nil {
		return DebugSessionSnapshot{}, errors.Join(err, ferretSession.Close())
	}

	m.mu.Lock()
	if m.closed || m.sessions[sessionID] != parent {
		m.mu.Unlock()
		parent.removeDebugSession(created)

		return DebugSessionSnapshot{}, errors.Join(ErrSessionClosed, ferretSession.Close())
	}
	m.debugSessions[id] = created
	m.mu.Unlock()

	return created.Snapshot(), nil
}

// GetDebugSession returns an immutable DebugSession snapshot.
func (m *Manager) GetDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.Snapshot(), nil
}

// StartDebugSession starts a DebugSession asynchronously.
func (m *Manager) StartDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.Start(ctx)
}

// ContinueDebugSession resumes a stopped DebugSession.
func (m *Manager) ContinueDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.Continue(ctx)
}

// PauseDebugSession requests a stop from a running DebugSession.
func (m *Manager) PauseDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.Pause(ctx)
}

// StepInDebugSession steps into the next logical source location.
func (m *Manager) StepInDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.StepIn(ctx)
}

// StepOverDebugSession steps over calls at the current depth.
func (m *Manager) StepOverDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.StepOver(ctx)
}

// StepOutDebugSession resumes until execution returns to a caller.
func (m *Manager) StepOutDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.StepOut(ctx)
}

// ReplaceDebugBreakpoints replaces all breakpoints for one source.
func (m *Manager) ReplaceDebugBreakpoints(
	ctx context.Context,
	id DebugSessionID,
	file string,
	locations []DebugBreakpointLocation,
) ([]DebugBreakpoint, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return debugSession.ReplaceBreakpoints(ctx, file, locations)
}

// DebugFrames returns the current-to-caller paused frame stack.
func (m *Manager) DebugFrames(ctx context.Context, id DebugSessionID) ([]DebugFrame, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return debugSession.Frames(ctx)
}

// DebugScopes returns Locals and Parameters for one paused frame.
func (m *Manager) DebugScopes(ctx context.Context, id DebugSessionID, frame int) ([]DebugScope, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return debugSession.Scopes(ctx, frame)
}

// DebugVariables expands one paused-state value reference.
func (m *Manager) DebugVariables(
	ctx context.Context,
	id DebugSessionID,
	reference DebugValueReference,
) ([]DebugVariable, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return nil, err
	}

	return debugSession.Variables(ctx, reference)
}

// EvaluateDebugSession evaluates an expression in one paused frame.
func (m *Manager) EvaluateDebugSession(
	ctx context.Context,
	id DebugSessionID,
	frame int,
	expression string,
) (DebugValue, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugValue{}, err
	}

	return debugSession.Evaluate(ctx, frame, expression)
}

// TerminateDebugSession idempotently requests termination and retains the resource.
func (m *Manager) TerminateDebugSession(ctx context.Context, id DebugSessionID) (DebugSessionSnapshot, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSessionSnapshot{}, err
	}

	return debugSession.Terminate(ctx)
}

// WatchDebugSession subscribes to current and future lifecycle events.
func (m *Manager) WatchDebugSession(ctx context.Context, id DebugSessionID) (DebugSubscription, error) {
	debugSession, err := m.debugSession(ctx, id)
	if err != nil {
		return DebugSubscription{}, err
	}

	return debugSession.Subscribe(), nil
}

// CloseDebugSession removes a DebugSession and guarantees eventual cleanup.
func (m *Manager) CloseDebugSession(ctx context.Context, id DebugSessionID) error {
	debugSession := m.detachDebugSession(id)
	if debugSession == nil {
		return nil
	}

	if debugSession.beginClose() {
		go m.finishDebugSessionClose(debugSession)
	}

	return waitForDone(ctx, debugSession.closeDone, debugSession.closeResult)
}

func (m *Manager) debugCompilationError(parent *Session, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrSessionClosed) || errors.Is(err, workspace.ErrClosed) ||
		errors.Is(err, workspace.ErrDocumentNotFound) {
		return err
	}

	return &CompilationError{
		Source:      parent.source,
		Diagnostics: diagnostic.FromError(string(parent.source.URI), parent.text, err),
		Cause:       err,
	}
}

func (m *Manager) debugSession(ctx context.Context, id DebugSessionID) (*DebugSession, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}

	m.mu.RLock()
	debugSession := m.debugSessions[id]
	m.mu.RUnlock()
	if debugSession == nil {
		return nil, ErrDebugSessionNotFound
	}

	return debugSession, nil
}

func (m *Manager) detachDebugSession(id DebugSessionID) *DebugSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	if debugSession := m.closingDebugs[id]; debugSession != nil {
		return debugSession
	}

	debugSession := m.debugSessions[id]
	if debugSession == nil {
		return nil
	}

	m.detachKnownDebugSessionLocked(debugSession)

	return debugSession
}

func (m *Manager) detachKnownDebugSession(debugSession *DebugSession) {
	m.mu.Lock()
	if m.debugSessions[debugSession.id] == debugSession {
		m.detachKnownDebugSessionLocked(debugSession)
	}
	m.mu.Unlock()
}

func (m *Manager) detachKnownDebugSessionLocked(debugSession *DebugSession) {
	delete(m.debugSessions, debugSession.id)
	m.closingDebugs[debugSession.id] = debugSession
}

func (m *Manager) finishDebugSessionClose(debugSession *DebugSession) {
	debugSession.settleClose()

	m.mu.Lock()
	parent := m.sessions[debugSession.session]
	if parent == nil {
		parent = m.closingSessions[debugSession.session]
	}
	if parent != nil {
		parent.removeDebugSession(debugSession)
	}
	m.mu.Unlock()

	debugSession.completeClose()

	m.mu.Lock()
	delete(m.closingDebugs, debugSession.id)
	m.mu.Unlock()
}
