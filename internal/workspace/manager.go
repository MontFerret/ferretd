// Package workspace manages the workspaces known to ferretd.
package workspace

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"
)

type (
	// Manager owns concurrency-safe, process-local workspace state.
	Manager struct {
		mu         sync.RWMutex
		byID       map[ID]*Workspace
		byRoot     map[string]ID
		opening    map[string]*openOperation
		load       loadWorkspaceFunc
		generation uint64
	}

	loadWorkspaceFunc func(context.Context, string) (workspaceContent, error)

	openOperation struct {
		generation uint64
		done       chan struct{}
		workspace  *Workspace
		err        error
	}
)

// New creates a workspace manager.
func New() *Manager {
	return &Manager{
		byID:    make(map[ID]*Workspace),
		byRoot:  make(map[string]ID),
		opening: make(map[string]*openOperation),
		load:    loadWorkspace,
	}
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
		existing, opening := m.find(canonical)
		if existing != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			return existing, nil
		}

		if opening != nil {
			return m.waitForOpen(ctx, opening)
		}

		candidateUUID, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("%w: generate workspace ID: %w", ErrLoad, err)
		}

		candidate := newWorkspace(ID(candidateUUID.String()), canonical)
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
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()

		return err
	}

	item, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()

		return nil
	}

	delete(m.byID, id)
	delete(m.byRoot, item.Root())
	m.mu.Unlock()

	item.close()

	return nil
}

// Clear removes and closes all retained workspace state.
func (m *Manager) Clear() {
	m.mu.Lock()
	items := make([]*Workspace, 0, len(m.byID))
	for _, item := range m.byID {
		items = append(items, item)
	}

	m.byID = make(map[ID]*Workspace)
	m.byRoot = make(map[string]ID)
	m.opening = make(map[string]*openOperation)
	m.generation++
	m.mu.Unlock()

	for _, item := range items {
		item.close()
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

	m.mu.Lock()
	if operation.generation != m.generation && err == nil {
		err = fmt.Errorf("workspace manager was cleared during load")
	}

	if err != nil {
		err = fmt.Errorf("%w: load %q: %w", ErrLoad, operation.workspace.Root(), err)
		operation.workspace.setFailed(err)
	} else {
		operation.workspace.setReady(content)
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
