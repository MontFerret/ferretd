// Package debug coordinates retained debugger-specific Sessions and their lifecycle.
package debug

import (
	"context"
	"errors"
	"sync"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
)

// Manager owns all process-local retained debug Sessions.
type Manager struct {
	mu sync.RWMutex

	executions *exec.Manager
	sessions   map[SessionID]*session
	closing    map[SessionID]*session
	groups     map[exec.SessionID]*sessionGroup
	closed     bool
}

// New creates a debug manager that borrows an execution manager.
// It returns an error when the execution manager is nil.
func New(executions *exec.Manager) (*Manager, error) {
	if executions == nil {
		return nil, errNilExecutionManager
	}

	result := &Manager{
		executions: executions,
		sessions:   make(map[SessionID]*session),
		closing:    make(map[SessionID]*session),
		groups:     make(map[exec.SessionID]*sessionGroup),
	}
	executions.RegisterSessionCloseHook(result.closeExecutionSession)

	return result, nil
}

// CreateSession creates one retained debugger child of an executable Session.
func (m *Manager) CreateSession(
	ctx context.Context,
	parentID exec.SessionID,
	parameters exec.Parameters,
	options exec.RuntimeOptions,
) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	id, err := newSessionID()
	if err != nil {
		return SessionSnapshot{}, err
	}

	if err := m.beginCreate(parentID); err != nil {
		return SessionSnapshot{}, err
	}

	defer m.finishCreate(parentID)

	runtime, err := m.executions.CreateDebugRuntime(ctx, parentID, parameters, options)
	if err != nil {
		return SessionSnapshot{}, err
	}

	created := newSession(id, runtime)
	m.mu.Lock()
	group := m.groups[parentID]
	if m.closed || group == nil || !group.gate.Accepting() {
		managerClosed := m.closed
		m.mu.Unlock()

		closeErr := runtime.Close()
		if managerClosed {
			return SessionSnapshot{}, errors.Join(ErrClosed, closeErr)
		}

		return SessionSnapshot{}, errors.Join(exec.ErrSessionClosed, closeErr)
	}

	m.sessions[id] = created
	group.sessions[id] = created
	m.mu.Unlock()

	return created.snapshot(), nil
}

// GetSession returns an immutable debug Session snapshot.
func (m *Manager) GetSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.snapshot(), nil
}

// StartSession starts a debug Session asynchronously.
func (m *Manager) StartSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.start(ctx)
}

// ContinueSession resumes a stopped debug Session.
func (m *Manager) ContinueSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.continueExecution(ctx)
}

// PauseSession requests a stop from a running debug Session.
func (m *Manager) PauseSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.pause(ctx)
}

// StepInSession steps into the next logical source location.
func (m *Manager) StepInSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.stepIn(ctx)
}

// StepOverSession steps over calls at the current depth.
func (m *Manager) StepOverSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.stepOver(ctx)
}

// StepOutSession resumes until execution returns to a caller.
func (m *Manager) StepOutSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.stepOut(ctx)
}

// ReplaceBreakpoints replaces all breakpoints for one source.
func (m *Manager) ReplaceBreakpoints(
	ctx context.Context,
	id SessionID,
	sourceName string,
	locations []apisource.Position,
) ([]apidebugger.Breakpoint, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return nil, err
	}

	return session.replaceBreakpoints(ctx, sourceName, locations)
}

// Frames returns the current-to-caller paused frame stack.
func (m *Manager) Frames(ctx context.Context, id SessionID) ([]apidebugger.Frame, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return nil, err
	}

	return session.frames(ctx)
}

// Scopes returns Locals and Parameters for one paused frame.
func (m *Manager) Scopes(ctx context.Context, id SessionID, frame int) ([]Scope, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return nil, err
	}

	return session.scopes(ctx, frame)
}

// Variables expands one paused-state value reference.
func (m *Manager) Variables(
	ctx context.Context,
	id SessionID,
	reference apidebugger.ValueReference,
) ([]apidebugger.Variable, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return nil, err
	}

	return session.variables(ctx, reference)
}

// Evaluate evaluates an expression in one paused frame.
func (m *Manager) Evaluate(
	ctx context.Context,
	id SessionID,
	frame int,
	expression string,
) (apidebugger.Value, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return apidebugger.Value{}, err
	}

	return session.evaluate(ctx, frame, expression)
}

// TerminateSession idempotently requests termination and retains the resource.
func (m *Manager) TerminateSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return session.terminateExecution(ctx)
}

// WatchSession subscribes to current and future lifecycle events.
func (m *Manager) WatchSession(ctx context.Context, id SessionID) (Subscription, error) {
	session, err := m.session(ctx, id)
	if err != nil {
		return Subscription{}, err
	}

	return session.subscribe(), nil
}

// CloseSession removes a debug Session and guarantees eventual cleanup.
func (m *Manager) CloseSession(ctx context.Context, id SessionID) error {
	session := m.detachSession(id)
	if session == nil {
		return nil
	}

	if session.beginClose() {
		go m.finishSessionClose(session)
	}

	return session.close.Wait(ctx)
}

// Close prevents new resources and settles every retained debug Session.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	parentIDs := make([]exec.SessionID, 0, len(m.groups))
	for id := range m.groups {
		parentIDs = append(parentIDs, id)
	}
	m.mu.Unlock()

	var result error
	for _, id := range parentIDs {
		result = errors.Join(result, m.closeExecutionSession(ctx, id))
	}

	return result
}

func (m *Manager) session(ctx context.Context, id SessionID) (*session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	session := m.sessions[id]
	m.mu.RUnlock()
	if session == nil {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

func (m *Manager) beginCreate(parentID exec.SessionID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}

	group := m.groups[parentID]
	if group == nil {
		group = &sessionGroup{sessions: make(map[SessionID]*session)}
		m.groups[parentID] = group
	}

	if !group.gate.BeginCreate() {
		return exec.ErrSessionClosed
	}

	return nil
}

func (m *Manager) finishCreate(parentID exec.SessionID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group := m.groups[parentID]
	if group == nil {
		return
	}

	if group.gate.EndCreate() && group.gate.Accepting() && len(group.sessions) == 0 {
		delete(m.groups, parentID)
	}
}

func (m *Manager) detachSession(id SessionID) *session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session := m.closing[id]; session != nil {
		return session
	}

	session := m.sessions[id]
	if session == nil {
		return nil
	}

	delete(m.sessions, id)
	m.closing[id] = session

	return session
}

func (m *Manager) finishSessionClose(session *session) {
	session.settleClose()
	session.completeClose()

	m.mu.Lock()
	if group := m.groups[session.runtime.SessionID()]; group != nil {
		if group.sessions[session.id] == session {
			delete(group.sessions, session.id)
		}

		if group.gate.Accepting() && group.gate.Idle() && len(group.sessions) == 0 {
			delete(m.groups, session.runtime.SessionID())
		}
	}
	delete(m.closing, session.id)
	m.mu.Unlock()
}

func (m *Manager) closeExecutionSession(ctx context.Context, parentID exec.SessionID) error {
	m.mu.Lock()
	group := m.groups[parentID]
	if group == nil {
		m.mu.Unlock()

		return nil
	}

	owner := group.gate.BeginClose()
	m.mu.Unlock()

	if owner {
		go m.finishExecutionSessionClose(parentID, group)
	}

	return group.gate.WaitClose(ctx)
}

func (m *Manager) finishExecutionSessionClose(parentID exec.SessionID, group *sessionGroup) {
	group.gate.WaitForCreates()

	m.mu.RLock()
	ids := make([]SessionID, 0, len(group.sessions))
	for id := range group.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	var result error
	for _, id := range ids {
		result = errors.Join(result, m.CloseSession(context.Background(), id))
	}

	group.gate.FinishClose(result)

	m.mu.Lock()
	if m.groups[parentID] == group {
		delete(m.groups, parentID)
	}
	m.mu.Unlock()
}
