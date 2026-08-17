package exec

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/diagnostic"
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
		groups            map[workspace.ID]*workspaceGroup
		closed            bool
	}

	workspaceGroup struct {
		closing      bool
		closeStarted bool
		closeDone    chan struct{}
		closeErr     error
		creating     int
		createDone   chan struct{}
		sessions     map[SessionID]*Session
	}
)

// New creates an execution manager parented by the existing workspace manager.
func New(workspaces *workspace.Manager) *Manager {
	if workspaces == nil {
		workspaces = workspace.New()
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

	return result
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

	compilation, err := parent.Compile(ctx, relativePath)
	if err != nil {
		if compilation.Plan != nil {
			err = errors.Join(err, compilation.Plan.Close())
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, workspace.ErrClosed) || errors.Is(err, workspace.ErrDocumentNotFound) {
			return SessionSnapshot{}, err
		}

		document, _ := parent.Document(relativePath)
		diagnostics := diagnostic.FromError(string(compilation.Source.URI), document.Content(), err)
		if errors.Is(err, workspace.ErrDocumentUnavailable) {
			mapper := source.NewMapper(document.Content())
			for _, item := range document.Diagnostics() {
				diagnostics = append(diagnostics, diagnostic.Convert(string(compilation.Source.URI), mapper, item))
			}
		}

		return SessionSnapshot{}, &CompilationError{
			Source:      compilation.Source,
			Diagnostics: diagnostics,
			Cause:       err,
		}
	}

	document, ok := parent.Document(relativePath)
	if !ok {
		return SessionSnapshot{}, errors.Join(workspace.ErrDocumentNotFound, compilation.Plan.Close())
	}

	id, err := newSessionID()
	if err != nil {
		return SessionSnapshot{}, errors.Join(err, compilation.Plan.Close())
	}

	created := newSession(id, compilation, document.Content())
	m.mu.Lock()
	group := m.groups[workspaceID]

	if m.closed || group == nil || group.closing {
		m.mu.Unlock()

		return SessionSnapshot{}, errors.Join(ErrWorkspaceClosed, compilation.Plan.Close())
	}

	if err := ctx.Err(); err != nil {
		m.mu.Unlock()

		return SessionSnapshot{}, errors.Join(err, compilation.Plan.Close())
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

// CloseSession idempotently removes a Session and all child Executions.
func (m *Manager) CloseSession(ctx context.Context, id SessionID) error {
	session, owner := m.detachSession(id)
	if session == nil {
		return nil
	}

	if owner {
		go m.finishSessionClose(session)
	}

	return waitForDone(ctx, session.closeDone, session.closeResult)
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

	params, err := runtime.NewParamsFrom(parameters)
	if err != nil {
		return ExecutionSnapshot{}, invalidParametersError(err)
	}

	id, err := newExecutionID()
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	options.OutputContentType = strings.TrimSpace(options.OutputContentType)
	if options.OutputContentType == "" {
		options.OutputContentType = "application/json"
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()

		return ExecutionSnapshot{}, ErrManagerClosed
	}

	session, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()

		return ExecutionSnapshot{}, ErrSessionNotFound
	}

	execution := newExecution(id, session, params, parameters, options)
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
	execution, owner := m.detachExecution(id)
	if execution == nil {
		return nil
	}

	if owner {
		go m.finishExecutionClose(execution)
	}

	return waitForDone(ctx, execution.closeDone, func() error { return nil })
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

	group.closing = true
	owner := !group.closeStarted
	if owner {
		group.closeStarted = true
		group.closeDone = make(chan struct{})
	}

	done := group.closeDone
	m.mu.Unlock()

	if owner {
		go m.finishWorkspaceClose(id, group)
	}

	return waitForDone(ctx, done, func() error { return group.closeErr })
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
	m.mu.RLock()
	creating := group.creating
	createDone := group.createDone
	m.mu.RUnlock()

	if creating > 0 {
		_ = waitForDone(context.Background(), createDone, func() error { return nil })
	}

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

	group.closeErr = result
	close(group.closeDone)

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
		return ErrManagerClosed
	}

	group := m.groups[id]
	if group == nil {
		group = &workspaceGroup{sessions: make(map[SessionID]*Session)}
		m.groups[id] = group
	}

	if group.closing {
		return ErrWorkspaceClosed
	}

	if group.creating == 0 {
		group.createDone = make(chan struct{})
	}

	group.creating++

	return nil
}

func (m *Manager) finishSessionCreate(id workspace.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	group := m.groups[id]
	if group == nil || group.creating == 0 {
		return
	}

	group.creating--
	if group.creating == 0 {
		close(group.createDone)

		group.createDone = nil

		if !group.closing && len(group.sessions) == 0 {
			delete(m.groups, id)
		}
	}
}

func (m *Manager) detachSession(id SessionID) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.closingSessions[id]; ok {
		return session, false
	}

	session, ok := m.sessions[id]
	if !ok {
		return nil, false
	}

	delete(m.sessions, id)

	if group := m.groups[session.source.Workspace]; group != nil {
		delete(group.sessions, id)
	}

	m.closingSessions[id] = session

	return session, true
}

func (m *Manager) finishSessionClose(session *Session) {
	executions, plan, owner := session.beginClose()
	if !owner {
		return
	}

	var result error
	for _, execution := range executions {
		m.detachKnownExecution(execution)

		if execution.beginClose() {
			go m.finishExecutionClose(execution)
		}

		if err := waitForDone(context.Background(), execution.closeDone, func() error { return nil }); err != nil {
			result = errors.Join(result, err)
		}
	}

	if plan != nil {
		result = errors.Join(result, plan.Close())
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

func (m *Manager) detachExecution(id ExecutionID) (*Execution, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if execution, ok := m.closingExecutions[id]; ok {
		return execution, false
	}

	execution, ok := m.executions[id]
	if !ok {
		return nil, false
	}

	m.detachKnownExecutionLocked(execution)

	return execution, true
}

func (m *Manager) detachKnownExecution(execution *Execution) {
	m.mu.Lock()
	if m.executions[execution.id] == execution {
		m.detachKnownExecutionLocked(execution)
	}
	m.mu.Unlock()
}

func (m *Manager) detachKnownExecutionLocked(execution *Execution) {
	delete(m.executions, execution.id)
	m.closingExecutions[execution.id] = execution

	if session := m.sessions[execution.session]; session != nil {
		session.mu.Lock()
		delete(session.executions, execution.id)
		session.mu.Unlock()
	}
}

func (m *Manager) finishExecutionClose(execution *Execution) {
	execution.finishClose()

	m.mu.Lock()
	delete(m.closingExecutions, execution.id)
	m.mu.Unlock()
}
