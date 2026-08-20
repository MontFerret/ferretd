// Package workspace manages the workspaces known to ferretd.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/MontFerret/ferret/v2"
)

type (
	// Manager owns concurrency-safe, process-local workspace state.
	Manager struct {
		mu         sync.RWMutex
		byID       map[ID]*Workspace
		byRoot     map[string]ID
		opening    map[string]*openOperation
		closing    map[ID]*closeOperation
		closeHooks []CloseHook
		load       loadWorkspaceFunc
		newEngine  engineFactory
		generation uint64
	}

	// CloseHook releases resources parented by a workspace before its Engine closes.
	CloseHook func(context.Context, ID) error

	loadWorkspaceFunc func(context.Context, string) (workspaceContent, error)

	engineFactory func(string) (*ferret.Engine, error)

	openOperation struct {
		generation uint64
		done       chan struct{}
		workspace  *Workspace
		err        error
	}

	closeOperation struct {
		done      chan struct{}
		workspace *Workspace
		err       error
	}
)

// New creates a workspace manager.
func New() *Manager {
	return &Manager{
		byID:    make(map[ID]*Workspace),
		byRoot:  make(map[string]ID),
		opening: make(map[string]*openOperation),
		closing: make(map[ID]*closeOperation),
		load:    loadWorkspace,
		newEngine: func(root string) (*ferret.Engine, error) {
			return ferret.New(ferret.WithFSRoot(root))
		},
	}
}

// RegisterCloseHook adds a workspace child-resource closer.
// Hooks must be registered during service construction, before concurrent use.
func (m *Manager) RegisterCloseHook(hook CloseHook) {
	if hook == nil {
		return
	}

	m.mu.Lock()
	m.closeHooks = append(m.closeHooks, hook)
	m.mu.Unlock()
}

// LookupDocument resolves an absolute path to the deepest matching open workspace.
// Paths are cleaned but symlinks are deliberately not resolved.
func (m *Manager) LookupDocument(ctx context.Context, absolutePath string) (DocumentLookup, bool, error) {
	if err := ctx.Err(); err != nil {
		return DocumentLookup{}, false, err
	}

	if absolutePath == "" || !filepath.IsAbs(absolutePath) {
		return DocumentLookup{}, false, fmt.Errorf("%w: document path must be absolute", ErrInvalidRoot)
	}

	path := filepath.Clean(absolutePath)
	m.mu.RLock()
	workspaces := make([]*Workspace, 0, len(m.byID))
	for _, item := range m.byID {
		workspaces = append(workspaces, item)
	}
	m.mu.RUnlock()

	sort.Slice(workspaces, func(i, j int) bool {
		left, right := workspaces[i].Root(), workspaces[j].Root()

		if len(left) != len(right) {
			return len(left) > len(right)
		}

		return left < right
	})

	for _, item := range workspaces {
		if err := ctx.Err(); err != nil {
			return DocumentLookup{}, false, err
		}

		relative, ok := pathWithinRoot(item.Root(), path)
		if !ok {
			continue
		}

		document, found := item.Document(relative)
		if !found {
			return DocumentLookup{}, false, nil
		}

		return DocumentLookup{
			Document:  document,
			Workspace: item.ID(),
			Revision:  document.Revision(),
		}, true, nil
	}

	return DocumentLookup{}, false, nil
}

func pathWithinRoot(root, candidate string) (string, bool) {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return "", false
	}

	prefix := ".." + string(filepath.Separator)
	if len(relative) >= len(prefix) && relative[:len(prefix)] == prefix {
		return "", false
	}

	return filepath.ToSlash(relative), true
}

// Open validates and synchronously loads a root, returning its shared workspace.
func (m *Manager) Open(ctx context.Context, root string) (*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	canonical, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		existing, opening := m.find(canonical)
		if existing != nil {
			return existing, nil
		}

		if opening != nil {
			workspace, err := m.waitForOpen(ctx, opening)
			if err == nil {
				return workspace, nil
			}

			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}

			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}

			return nil, err
		}

		candidateID, err := newID()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrLoad, err)
		}

		candidate := newWorkspace(candidateID, canonical)
		operation, owner := m.beginOpen(candidate)
		if !owner {
			continue
		}

		return m.loadAndCommit(ctx, operation)
	}
}

// Get returns the shared workspace by ID.
func (m *Manager) Get(ctx context.Context, id ID) (*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	result, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}

	return result, nil
}

// List returns shared workspaces ordered by root.
func (m *Manager) List(ctx context.Context) ([]*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	result := make([]*Workspace, 0, len(m.byID))
	for _, item := range m.byID {
		result = append(result, item)
	}
	m.mu.RUnlock()

	sort.Slice(result, func(left, right int) bool {
		return result[left].Root() < result[right].Root()
	})

	return result, nil
}

// Close removes a workspace and releases its retained source state.
func (m *Manager) Close(ctx context.Context, id ID) error {
	operation, owner := m.beginClose(id)
	if operation == nil {
		return nil
	}

	if owner {
		go m.finishClose(id, operation)
	}

	return waitForClose(ctx, operation)
}

// Clear removes and closes all retained workspace state.
func (m *Manager) Clear(ctx context.Context) error {
	m.mu.Lock()
	operations := make([]*closeOperation, 0, len(m.byID)+len(m.closing))
	owned := make(map[ID]*closeOperation, len(m.byID))

	for id, item := range m.byID {
		operation := &closeOperation{done: make(chan struct{}), workspace: item}
		item.markClosing()
		m.closing[id] = operation
		operations = append(operations, operation)
		owned[id] = operation
	}

	for id, operation := range m.closing {
		if owned[id] == operation {
			continue
		}

		operations = append(operations, operation)
	}

	m.byID = make(map[ID]*Workspace)
	m.byRoot = make(map[string]ID)
	m.opening = make(map[string]*openOperation)
	m.generation++
	m.mu.Unlock()

	for id, operation := range owned {
		go m.finishClose(id, operation)
	}

	var result error
	for _, operation := range operations {
		if err := waitForClose(ctx, operation); err != nil {
			result = errors.Join(result, err)
		}
	}

	return result
}

func (m *Manager) beginClose(id ID) (*closeOperation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if operation, ok := m.closing[id]; ok {
		return operation, false
	}

	item, ok := m.byID[id]
	if !ok {
		return nil, false
	}

	operation := &closeOperation{done: make(chan struct{}), workspace: item}
	item.markClosing()
	m.closing[id] = operation
	delete(m.byID, id)
	delete(m.byRoot, item.Root())

	return operation, true
}

func (m *Manager) finishClose(id ID, operation *closeOperation) {
	m.mu.RLock()
	hooks := append([]CloseHook(nil), m.closeHooks...)
	m.mu.RUnlock()

	var result error
	for _, hook := range hooks {
		if err := hook(context.Background(), id); err != nil {
			result = errors.Join(result, err)
		}
	}

	result = errors.Join(result, operation.workspace.close())
	operation.err = result
	close(operation.done)

	m.mu.Lock()
	if m.closing[id] == operation {
		delete(m.closing, id)
	}
	m.mu.Unlock()
}

func waitForClose(ctx context.Context, operation *closeOperation) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-operation.done:
		return operation.err
	}
}

func (m *Manager) find(root string) (*Workspace, *openOperation) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id, ok := m.byRoot[root]; ok {
		return m.byID[id], nil
	}

	return nil, m.opening[root]
}

func (m *Manager) beginOpen(candidate *Workspace) (*openOperation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.byRoot[candidate.Root()]; ok {
		return nil, false
	}

	if operation, ok := m.opening[candidate.Root()]; ok {
		return operation, false
	}

	operation := &openOperation{
		generation: m.generation,
		done:       make(chan struct{}),
		workspace:  candidate,
	}
	m.opening[candidate.Root()] = operation

	return operation, true
}

func (m *Manager) loadAndCommit(ctx context.Context, operation *openOperation) (*Workspace, error) {
	content, err := m.load(ctx, operation.workspace.Root())
	if err == nil {
		err = ctx.Err()
	}

	var engine *ferret.Engine
	if err == nil {
		engine, err = m.newEngine(operation.workspace.Root())
		if err != nil {
			err = fmt.Errorf("create workspace engine: %w", err)
		}
	}

	if err == nil {
		err = ctx.Err()
	}

	m.mu.Lock()
	if operation.generation != m.generation && err == nil {
		err = fmt.Errorf("workspace manager was cleared during load")
	}

	if err != nil {
		if engine != nil {
			err = errors.Join(err, engine.Close())
		}
		err = fmt.Errorf("%w: load %q: %w", ErrLoad, operation.workspace.Root(), err)
		operation.workspace.setFailed(err)
	} else {
		operation.workspace.setReady(content, engine)
	}

	if m.opening[operation.workspace.Root()] == operation {
		delete(m.opening, operation.workspace.Root())
	}
	operation.err = err
	if err == nil {
		m.byID[operation.workspace.ID()] = operation.workspace
		m.byRoot[operation.workspace.Root()] = operation.workspace.ID()
	}
	close(operation.done)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return operation.workspace, nil
}

func (m *Manager) waitForOpen(ctx context.Context, operation *openOperation) (*Workspace, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-operation.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if operation.err != nil {
			return nil, operation.err
		}

		return operation.workspace, nil
	}
}
