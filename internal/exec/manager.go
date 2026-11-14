// Package exec coordinates daemon-owned Universal Plans and one-shot Executions.
package exec

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	// Manager orchestrates process-local daemon Sessions and Executions.
	Manager struct {
		workspaces *workspace.Manager
		runtimes   *runtimes.Owner
		sessions   *sessionRegistry
		executions *executionRegistry

		hooksMu    sync.RWMutex
		closeHooks []SessionCloseHook
	}

	// SessionCloseHook releases sibling resources parented by an executable Session.
	SessionCloseHook func(context.Context, SessionID) error
)

// New creates an execution manager that borrows the workspace manager and runtime acquisition owner.
// It returns an error when either required dependency is nil.
func New(workspaces *workspace.Manager, owner *runtimes.Owner) (*Manager, error) {
	if workspaces == nil {
		return nil, errNilWorkspaceManager
	}

	if owner == nil {
		return nil, errNilRuntimeOwner
	}

	result := &Manager{
		workspaces: workspaces,
		runtimes:   owner,
		sessions:   newSessionRegistry(),
		executions: newExecutionRegistry(),
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

	m.hooksMu.Lock()
	m.closeHooks = append(m.closeHooks, hook)
	m.hooksMu.Unlock()
}

// CreateSession compiles one immutable workspace document into a reusable Plan.
func (m *Manager) CreateSession(
	ctx context.Context,
	workspaceID workspace.ID,
	relativePath string,
	runtimeName string,
) (SessionSnapshot, error) {
	return m.AcquireSession(ctx, workspaceID, relativePath, runtimeName, "")
}

// AcquireSession associates creation and its rollback with an optional client acquisition key.
func (m *Manager) AcquireSession(ctx context.Context, workspaceID workspace.ID, relativePath, runtimeName, acquisitionID string) (snapshot SessionSnapshot, resultErr error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	operation, operationCtx := newConstruction(ctx)
	defer operation.finish()

	creation, err := m.sessions.beginCreate(workspaceID, operation, acquisitionID)
	if err != nil {
		return SessionSnapshot{}, err
	}

	defer func() {
		closedErr := m.sessions.finishCreate(creation)

		if resultErr != nil {
			resultErr = errors.Join(resultErr, closedErr, constructionError(ctx), constructionError(operationCtx))
		}
	}()

	created, err := m.prepareSession(operationCtx, operation, workspaceID, relativePath, runtimeName)
	if err != nil {
		return SessionSnapshot{}, err
	}

	if err := m.sessions.commitCreate(operationCtx, creation, created); err != nil {
		return SessionSnapshot{}, errors.Join(err, operation.recordCleanup(created.closeUnpublished()))
	}

	return created.snapshot(), nil
}

func (m *Manager) prepareSession(
	ctx context.Context,
	operation *construction,
	workspaceID workspace.ID,
	relativePath string,
	runtimeName string,
) (created *session, resultErr error) {
	parent, err := m.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	document, err := parent.RefreshDocument(ctx, relativePath)
	if err != nil {
		return nil, err
	}

	file := document.File()
	sourceSnapshot := workspace.SourceSnapshot{
		Workspace:    workspaceID,
		RelativePath: file.RelativePath,
		URI:          file.URI,
		Revision:     document.Revision(),
	}
	text := document.Content()

	if !document.Loaded() {
		compileErr := fmt.Errorf("%w: %s", workspace.ErrDocumentUnavailable, file.RelativePath)

		return nil, &CompilationError{
			Source:      sourceSnapshot,
			Diagnostics: document.ProjectDiagnostics(),
			Cause:       compileErr,
		}
	}

	apiSource := api.NewSource(file.Path, text)

	binding, err := m.runtimes.Acquire(ctx, runtimeName)
	if err != nil {
		var rollback *runtimes.RollbackError
		if errors.As(err, &rollback) {
			_ = operation.recordCleanup(rollback)
		}

		return nil, err
	}

	var plan api.Plan
	defer func() {
		if created == nil {
			if plan != nil {
				resultErr = errors.Join(resultErr, operation.recordCleanup(plan.Close()))
			}

			resultErr = errors.Join(resultErr, operation.recordCleanup(binding.Release()))
		}
	}()

	plan, err = binding.Runtime().Compile(ctx, apiSource)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, workspace.ErrClosed) || errors.Is(err, workspace.ErrDocumentNotFound) {
			return nil, err
		}

		return nil, &CompilationError{
			Source:      sourceSnapshot,
			Diagnostics: diagnostic.FromError(sourceSnapshot.URI, text, err),
			Cause:       err,
		}
	}

	if plan == nil {
		err = errors.New("runtime returned no plan")

		return nil, &CompilationError{Source: sourceSnapshot, Cause: err}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	parameters, err := plan.Params(ctx)
	if err != nil {
		err = fmt.Errorf("retrieve plan parameters: %w", err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		return nil, &CompilationError{
			Source:      sourceSnapshot,
			Diagnostics: diagnostic.FromError(sourceSnapshot.URI, text, err),
			Cause:       err,
		}
	}

	id, err := newSessionID()
	if err != nil {
		return nil, err
	}

	compileDebug := func(ctx context.Context) (api.Plan, error) {
		return binding.Runtime().CompileDebug(ctx, apiSource)
	}

	fsRoot := ""

	if binding.Provider() == runtimes.ProviderNative {
		fsRoot = parent.Root()
	}

	return newSession(id, sourceSnapshot, plan, parameters, text, fsRoot, compileDebug, binding), nil
}

// GetSession returns an immutable Session snapshot.
func (m *Manager) GetSession(ctx context.Context, id SessionID) (SessionSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return SessionSnapshot{}, err
	}

	session := m.sessions.active(id)
	if session == nil {
		return SessionSnapshot{}, ErrSessionNotFound
	}

	return session.snapshot(), nil
}

// CloseSession idempotently removes a Session and all child resources.
func (m *Manager) CloseSession(ctx context.Context, id SessionID) error {
	return m.closeSession(ctx, id, nil)
}

func (m *Manager) closeSession(ctx context.Context, id SessionID, retained *sessionEntry) error {
	entry, owner := m.sessions.beginClose(id, retained)
	if entry == nil {
		return nil
	}

	if owner {
		go m.finishSessionClose(entry)
	}

	return entry.session.waitClose(ctx)
}

// CreateExecution creates a CREATED one-shot invocation resource.
func (m *Manager) CreateExecution(
	ctx context.Context,
	sessionID SessionID,
	parameters Parameters,
	options RuntimeOptions,
) (snapshot ExecutionSnapshot, resultErr error) {
	if err := ctx.Err(); err != nil {
		return ExecutionSnapshot{}, err
	}

	id, err := newExecutionID()
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	operation, operationCtx := newConstruction(ctx)
	defer operation.finish()

	creation, err := m.sessions.beginRuntimeCreate(sessionID, operation)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	defer func() {
		closedErr := creation.finish()

		if resultErr != nil {
			resultErr = errors.Join(resultErr, closedErr, constructionError(ctx), constructionError(operationCtx))
		}
	}()

	parent := creation.session()

	input, err := parent.prepareInput(parameters, options)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	if err := parent.publicationError(operationCtx); err != nil {
		return ExecutionSnapshot{}, err
	}

	runtime := newExecutionRuntime(parent.runtimeTarget(), input)
	execution := newExecution(id, runtime)
	m.executions.add(execution, parent)

	return execution.snapshot(), nil
}

// RunExecution starts a one-shot invocation and returns its RUNNING snapshot.
func (m *Manager) RunExecution(ctx context.Context, id ExecutionID) (ExecutionSnapshot, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	return execution.start(ctx)
}

// GetExecution returns an immutable Execution snapshot.
func (m *Manager) GetExecution(ctx context.Context, id ExecutionID) (ExecutionSnapshot, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	return execution.snapshot(), nil
}

// CancelExecution idempotently requests cancellation of an active Execution.
func (m *Manager) CancelExecution(ctx context.Context, id ExecutionID) (ExecutionSnapshot, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return ExecutionSnapshot{}, err
	}

	return execution.cancel(), nil
}

// CloseExecution idempotently removes and settles an Execution.
func (m *Manager) CloseExecution(ctx context.Context, id ExecutionID) error {
	closing := m.executions.beginClose(id)
	if closing.entry == nil {
		return nil
	}

	if closing.owner {
		go m.finishExecutionClose(closing.entry)
	}

	return closing.entry.execution.close.Wait(ctx)
}

// WatchExecution subscribes to current and future lifecycle events.
func (m *Manager) WatchExecution(ctx context.Context, id ExecutionID) (Subscription, error) {
	execution, err := m.execution(ctx, id)
	if err != nil {
		return Subscription{}, err
	}

	return execution.subscribe(), nil
}

// CloseWorkspace closes all Sessions parented by one workspace.
func (m *Manager) CloseWorkspace(ctx context.Context, id workspace.ID) error {
	closing := m.sessions.beginWorkspaceClose(id)
	if closing.owner {
		go m.finishWorkspaceClose(closing)
	}

	return closing.group.waitClose(ctx)
}

// Close prevents new resources and settles all current Sessions and Executions.
func (m *Manager) Close(ctx context.Context) error {
	groups := m.sessions.beginShutdown()
	for _, closing := range groups {
		if closing.owner {
			go m.finishWorkspaceClose(closing)
		}
	}

	var result error
	for _, closing := range groups {
		result = errors.Join(result, closing.group.waitClose(ctx))
	}

	return result
}

func (m *Manager) finishWorkspaceClose(closing workspaceClose) {
	closing.group.cancelPending()
	result := closing.group.waitForCreates()

	for _, entry := range closing.group.retainedSessions() {
		result = errors.Join(
			result,
			m.closeSession(context.Background(), entry.session.id, entry),
		)
	}

	m.sessions.finishWorkspaceClose(closing, result)
}

func (m *Manager) finishSessionClose(entry *sessionEntry) {
	session := entry.session
	session.cancelPending()
	result := session.waitForRuntimeCreates()

	children := m.executions.beginSessionClose(session.id)
	for _, closing := range children {
		if closing.owner {
			go m.finishExecutionClose(closing.entry)
		}
	}

	for _, closing := range children {
		result = errors.Join(result, closing.entry.execution.close.Wait(context.Background()))
	}

	m.executions.finishSessionClose(session.id)

	for _, hook := range m.sessionCloseHooks() {
		result = errors.Join(result, hook(context.Background(), session.id))
	}

	plan, debugPlan := session.releasePlans()
	if plan != nil {
		result = errors.Join(result, plan.Close())
	}

	if debugPlan != nil {
		result = errors.Join(result, debugPlan.Close())
	}

	result = errors.Join(result, session.binding.Release())

	m.sessions.finishClose(entry, result)
}

func (m *Manager) finishExecutionClose(entry *executionEntry) {
	closeErr := entry.execution.settleClose()
	entry.execution.completeClose(closeErr)
	m.executions.finishClose(entry)
}

func (m *Manager) execution(ctx context.Context, id ExecutionID) (*execution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	execution := m.executions.active(id)
	if execution == nil {
		return nil, ErrExecutionNotFound
	}

	return execution, nil
}

func (m *Manager) sessionCloseHooks() []SessionCloseHook {
	m.hooksMu.RLock()
	defer m.hooksMu.RUnlock()

	return append([]SessionCloseHook(nil), m.closeHooks...)
}
