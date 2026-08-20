// Package exec coordinates daemon-owned Ferret Plans and one-shot Executions.
package exec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/lifecycle"
	daemonparams "github.com/MontFerret/ferretd/internal/params"
	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	// Manager owns all process-local daemon Sessions and Executions.
	Manager struct {
		mu sync.RWMutex

		workspaces        *workspace.Manager
		sessions          map[SessionID]*Session
		closingSessions   map[SessionID]*Session
		executions        map[ExecutionID]*Execution
		closingExecutions map[ExecutionID]*Execution
		closeHooks        []SessionCloseHook
		groups            map[workspace.ID]*workspaceGroup
		closed            bool
	}

	// SessionCloseHook releases sibling resources parented by an executable Session.
	SessionCloseHook func(context.Context, SessionID) error

	workspaceGroup struct {
		// Manager.mu is acquired before gate when both are needed. Gate never
		// calls back into the Manager, so the lock order cannot reverse.
		gate     lifecycle.Gate
		sessions map[SessionID]*Session
	}
)

// New creates an execution manager that borrows the existing workspace manager.
// It returns an error when the workspace manager is nil.
func New(workspaces *workspace.Manager) (*Manager, error) {
	if workspaces == nil {
		return nil, errNilWorkspaceManager
	}

	result := &Manager{
		workspaces:        workspaces,
		sessions:          make(map[SessionID]*Session),
		closingSessions:   make(map[SessionID]*Session),
		executions:        make(map[ExecutionID]*Execution),
		closingExecutions: make(map[ExecutionID]*Execution),
		groups:            make(map[workspace.ID]*workspaceGroup),
	}
	workspaces.RegisterCloseHook(result.CloseWorkspace)

	return result, nil
}

// RegisterSessionCloseHook adds a Session child-resource closer.
// Hooks must be registered during service construction, before concurrent use.
func (m *Manager) RegisterSessionCloseHook(hook SessionCloseHook) {
	if hook == nil {
		return
	}

	m.mu.Lock()
	m.closeHooks = append(m.closeHooks, hook)
	m.mu.Unlock()
}

// CreateSession compiles one immutable workspace document into a reusable Plan.
func (m *Manager) CreateSession(
	ctx context.Context,
	workspaceID workspace.ID,
	relativePath string,
) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	if err := m.beginSessionCreate(workspaceID); err != nil {
		return SessionSnapshot{}, err
	}

	defer m.finishSessionCreate(workspaceID)

	parent, err := m.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	document, err := parent.RefreshDocument(ctx, relativePath)
	if err != nil {
		return SessionSnapshot{}, err
	}

	compilation, err := parent.CompileDocument(ctx, document)
	if err != nil {
		err = errors.Join(err, compilation.Close())

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, workspace.ErrClosed) || errors.Is(err, workspace.ErrDocumentNotFound) {
			return SessionSnapshot{}, err
		}

		diagnostics := diagnostic.FromError(compilation.Source.URI, document.Content(), err)
		if errors.Is(err, workspace.ErrDocumentUnavailable) {
			mapper := source.NewMapper(document.Content())
			for _, item := range document.Diagnostics() {
				diagnostics = append(diagnostics, diagnostic.Convert(compilation.Source.URI, mapper, item))
			}
		}

		return SessionSnapshot{}, &CompilationError{
			Source:      compilation.Source,
			Diagnostics: diagnostics,
			Cause:       err,
		}
	}

	id, err := newSessionID()
	if err != nil {
		return SessionSnapshot{}, errors.Join(err, compilation.Close())
	}

	created := newSession(id, parent, compilation, document.Content())
	m.mu.Lock()
	group := m.groups[workspaceID]

	if m.closed || group == nil || !group.gate.Accepting() {
		m.mu.Unlock()

		return SessionSnapshot{}, errors.Join(workspace.ErrClosed, compilation.Close())
	}

	if err := ctx.Err(); err != nil {
		m.mu.Unlock()

		return SessionSnapshot{}, errors.Join(err, compilation.Close())
	}

	m.sessions[id] = created
	group.sessions[id] = created
	m.mu.Unlock()

	return created.Snapshot(), nil
}

// GetSession returns an immutable Session snapshot.
func (m *Manager) GetSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return SessionSnapshot{}, ErrSessionNotFound
	}

	return session.Snapshot(), nil
}

// CloseSession idempotently removes a Session and all child resources.
func (m *Manager) CloseSession(ctx context.Context, id SessionID) error {
	session, executions, owner := m.detachSession(id)
	if session == nil {
		return nil
	}

	if owner {
		go m.finishSessionClose(session, executions)
	}

	return session.close.Wait(ctx)
}

// CreateExecution creates a CREATED one-shot invocation resource.
func (m *Manager) CreateExecution(
	ctx context.Context,
	sessionID SessionID,
	parameters map[string]any,
	options ExecutionOptions,
) (ExecutionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionSnapshot{}, err
	}

	runtimeParams, retainedParameters, err := daemonparams.Prepare(parameters)
	if err != nil {
		return ExecutionSnapshot{}, fmt.Errorf("%w: %v", ErrInvalidParameters, err)
	}

	id, err := newExecutionID()
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	options = options.normalized()

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()

		return ExecutionSnapshot{}, ErrClosed
	}

	session, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()

		return ExecutionSnapshot{}, ErrSessionNotFound
	}

	execution := newExecution(id, session, runtimeParams, retainedParameters, options)
	if err := session.addExecution(execution); err != nil {
		m.mu.Unlock()

		return ExecutionSnapshot{}, err
	}

	m.executions[id] = execution
	m.mu.Unlock()

	return execution.Snapshot(), nil
}

// RunExecution starts a one-shot invocation and returns its RUNNING snapshot.
func (m *Manager) RunExecution(ctx context.Context, id ExecutionID) (ExecutionSnapshot, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	return execution.Start(ctx)
}

// GetExecution returns an immutable Execution snapshot.
func (m *Manager) GetExecution(ctx context.Context, id ExecutionID) (ExecutionSnapshot, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	return execution.Snapshot(), nil
}

// CancelExecution idempotently requests cancellation of an active Execution.
func (m *Manager) CancelExecution(ctx context.Context, id ExecutionID) (ExecutionSnapshot, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	return execution.Cancel(), nil
}

// CloseExecution idempotently removes and settles an Execution.
func (m *Manager) CloseExecution(ctx context.Context, id ExecutionID) error {
	execution := m.detachExecution(id)
	if execution == nil {
		return nil
	}

	if execution.beginClose() {
		go m.finishExecutionClose(execution)
	}

	return execution.close.Wait(ctx)
}

// WatchExecution subscribes to current and future lifecycle events.
func (m *Manager) WatchExecution(ctx context.Context, id ExecutionID) (Subscription, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return Subscription{}, err
	}

	return execution.Subscribe(), nil
}

// CloseWorkspace closes all Sessions parented by one workspace.
func (m *Manager) CloseWorkspace(ctx context.Context, id workspace.ID) error {
	m.mu.Lock()
	group := m.groups[id]

	if group == nil {
		group = &workspaceGroup{sessions: make(map[SessionID]*Session)}
		m.groups[id] = group
	}

	owner := group.gate.BeginClose()
	m.mu.Unlock()

	if owner {
		go m.finishWorkspaceClose(id, group)
	}

	return group.gate.WaitClose(ctx)
}

// Close prevents new resources and settles all current Sessions and Executions.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
	}

	workspaceIDs := make([]workspace.ID, 0, len(m.groups))
	for id := range m.groups {
		workspaceIDs = append(workspaceIDs, id)
	}
	m.mu.Unlock()

	var result error
	for _, id := range workspaceIDs {
		if err := m.CloseWorkspace(ctx, id); err != nil {
			result = errors.Join(result, err)
		}
	}

	return result
}

func (m *Manager) finishWorkspaceClose(id workspace.ID, group *workspaceGroup) {
	group.gate.WaitForCreates()

	m.mu.RLock()
	ids := make([]SessionID, 0, len(group.sessions))
	for sessionID := range group.sessions {
		ids = append(ids, sessionID)
	}
	m.mu.RUnlock()

	var result error
	for _, sessionID := range ids {
		result = errors.Join(result, m.CloseSession(context.Background(), sessionID))
	}

	group.gate.FinishClose(result)

	m.mu.Lock()
	if m.groups[id] == group {
		delete(m.groups, id)
	}
	m.mu.Unlock()
}

func (m *Manager) beginSessionCreate(id workspace.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrClosed
	}

	group := m.groups[id]
	if group == nil {
		group = &workspaceGroup{sessions: make(map[SessionID]*Session)}
		m.groups[id] = group
	}

	if !group.gate.BeginCreate() {
		return workspace.ErrClosed
	}

	return nil
}

func (m *Manager) finishSessionCreate(id workspace.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group := m.groups[id]
	if group == nil {
		return
	}

	if group.gate.EndCreate() && group.gate.Accepting() && len(group.sessions) == 0 {
		delete(m.groups, id)
	}
}

func (m *Manager) detachSession(id SessionID) (*Session, []*Execution, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.closingSessions[id]; ok {
		return session, nil, false
	}

	session, ok := m.sessions[id]
	if !ok {
		return nil, nil, false
	}
	executions, owner := session.beginClose()
	if !owner {
		return session, nil, false
	}

	delete(m.sessions, id)

	if group := m.groups[session.source.Workspace]; group != nil {
		delete(group.sessions, id)
	}

	m.closingSessions[id] = session

	return session, executions, true
}

func (m *Manager) finishSessionClose(session *Session, executions []*Execution) {
	var result error
	for _, execution := range executions {
		m.detachKnownExecution(execution)

		if execution.beginClose() {
			go m.finishExecutionClose(execution)
		}

		if err := execution.close.Wait(context.Background()); err != nil {
			result = errors.Join(result, err)
		}
	}

	m.mu.RLock()
	hooks := append([]SessionCloseHook(nil), m.closeHooks...)
	m.mu.RUnlock()
	for _, hook := range hooks {
		if err := hook(context.Background(), session.id); err != nil {
			result = errors.Join(result, err)
		}
	}

	plan, debugPlan := session.releasePlans()

	if plan != nil {
		result = errors.Join(result, plan.Close())
	}
	if debugPlan != nil {
		result = errors.Join(result, debugPlan.Close())
	}

	session.finishClose(result)
	m.mu.Lock()
	delete(m.closingSessions, session.id)
	m.mu.Unlock()
}

func (m *Manager) execution(ctx context.Context, id ExecutionID) (*Execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	execution, ok := m.executions[id]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrExecutionNotFound
	}

	return execution, nil
}

func (m *Manager) detachExecution(id ExecutionID) *Execution {
	m.mu.Lock()
	defer m.mu.Unlock()

	if execution, ok := m.closingExecutions[id]; ok {
		return execution
	}

	execution, ok := m.executions[id]
	if !ok {
		return nil
	}

	m.detachKnownExecutionLocked(execution)

	return execution
}

func (m *Manager) detachKnownExecution(execution *Execution) {
	m.mu.Lock()
	if m.executions[execution.id] == execution {
		m.detachKnownExecutionLocked(execution)
	}
	m.mu.Unlock()
}

func (m *Manager) detachKnownExecutionLocked(execution *Execution) {
	// Public visibility ends immediately, while Session ownership lasts through cleanup.
	delete(m.executions, execution.id)
	m.closingExecutions[execution.id] = execution
}

func (m *Manager) finishExecutionClose(execution *Execution) {
	execution.settleClose()

	m.mu.Lock()
	session := m.sessions[execution.session]
	if session == nil {
		session = m.closingSessions[execution.session]
	}

	if session != nil {
		session.removeExecution(execution)
	}

	m.mu.Unlock()

	execution.completeClose()

	m.mu.Lock()
	delete(m.closingExecutions, execution.id)
	m.mu.Unlock()
}
