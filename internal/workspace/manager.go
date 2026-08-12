// Package workspace manages the workspaces known to ferretd.
package workspace

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// Manager owns concurrency-safe, process-local workspace state.
type Manager struct {
	mu     sync.RWMutex
	byID   map[ID]Workspace
	byRoot map[string]ID
}

// New creates a workspace manager.
func New() *Manager {
	return &Manager{
		byID:   make(map[ID]Workspace),
		byRoot: make(map[string]ID),
	}
}

// Open validates a root and returns its existing or newly-created workspace.
func (m *Manager) Open(ctx context.Context, root string) (Workspace, error) {
	if err := ctx.Err(); err != nil {
		return Workspace{}, err
	}

	canonical, err := canonicalRoot(root)
	if err != nil {
		return Workspace{}, err
	}

	if err := ctx.Err(); err != nil {
		return Workspace{}, err
	}

	m.mu.RLock()
	if id, ok := m.byRoot[canonical]; ok {
		result := m.byID[id]
		m.mu.RUnlock()

		return result, nil
	}
	m.mu.RUnlock()

	candidateUUID, err := uuid.NewRandom()
	if err != nil {
		return Workspace{}, fmt.Errorf("generate workspace ID: %w", err)
	}

	candidateID := ID(candidateUUID.String())

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Workspace{}, err
	}

	if id, ok := m.byRoot[canonical]; ok {
		return m.byID[id], nil
	}

	result := Workspace{
		ID:   candidateID,
		Root: canonical,
	}
	m.byID[result.ID] = result
	m.byRoot[canonical] = result.ID

	return result, nil
}

// Get returns a workspace snapshot by ID.
func (m *Manager) Get(ctx context.Context, id ID) (Workspace, error) {
	if err := ctx.Err(); err != nil {
		return Workspace{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	result, ok := m.byID[id]
	if !ok {
		return Workspace{}, ErrNotFound
	}

	return result, nil
}

// List returns workspace snapshots ordered by root.
func (m *Manager) List(ctx context.Context) ([]Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	result := make([]Workspace, 0, len(m.byID))
	for _, item := range m.byID {
		result = append(result, item)
	}
	m.mu.RUnlock()

	sort.Slice(result, func(left, right int) bool {
		return result[left].Root < result[right].Root
	})

	return result, nil
}

// Close removes a workspace. Closing an unknown workspace is safe.
func (m *Manager) Close(ctx context.Context, id ID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	item, ok := m.byID[id]
	if !ok {
		return nil
	}

	delete(m.byID, id)
	delete(m.byRoot, item.Root)

	return nil
}

// Clear removes all workspace state.
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.byID = make(map[ID]Workspace)
	m.byRoot = make(map[string]ID)
}
