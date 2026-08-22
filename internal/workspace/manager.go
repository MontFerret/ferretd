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
	"github.com/MontFerret/ferretd/internal/lifecycle"
)

type (
	// Manager owns concurrency-safe, process-local workspace state.
	Manager struct {
		mu            sync.RWMutex
		byID          map[ID]*workspaceEntry
		byRoot        map[string]ID
		opening       map[string]*openOperation
		closeHooks    []CloseHook
		loadWorkspace workspaceLoader
		newEngine     newEngineFunc
		newWatcher    workspaceWatcherFactory
		startWatcher  func(*workspaceWatcher, *Workspace)
		generation    uint64
	}

	// CloseHook releases resources parented by a workspace before its Engine closes.
	CloseHook func(context.Context, ID) error

	workspaceLoader func(context.Context, string, directoryObserver) (workspaceContent, error)

	newEngineFunc func(string) (*ferret.Engine, error)

	openOperation struct {
		generation uint64
		done       chan struct{}
		workspace  *Workspace
		err        error
	}

	workspaceEntryState uint8

	workspaceEntry struct {
		workspace *Workspace
		state     workspaceEntryState
		close     lifecycle.CloseOperation
	}
)

const (
	workspaceEntryActive workspaceEntryState = iota + 1
	workspaceEntryClosing
)

// New creates a workspace manager.
func New() *Manager {
	return &Manager{
		byID:          make(map[ID]*workspaceEntry),
		byRoot:        make(map[string]ID),
		opening:       make(map[string]*openOperation),
		loadWorkspace: loadWorkspace,
		newEngine: func(root string) (*ferret.Engine, error) {
			return ferret.New(ferret.WithFSRoot(root))
		},
		newWatcher: newWorkspaceWatcher,
		startWatcher: func(watcher *workspaceWatcher, workspace *Workspace) {
			watcher.Start(workspace)
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
	for _, entry := range m.byID {
		if entry.state == workspaceEntryActive {
			workspaces = append(workspaces, entry.workspace)
		}
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
			Document:   document,
			Workspace:  item.ID(),
			Revision:   document.Revision(),
			Generation: document.generation,
		}, true, nil
	}

	return DocumentLookup{}, false, nil
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

	entry := m.byID[id]
	if entry == nil || entry.state != workspaceEntryActive {
		return nil, ErrNotFound
	}

	return entry.workspace, nil
}

// List returns shared workspaces ordered by root.
func (m *Manager) List(ctx context.Context) ([]*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	result := make([]*Workspace, 0, len(m.byID))
	for _, entry := range m.byID {
		if entry.state == workspaceEntryActive {
			result = append(result, entry.workspace)
		}
	}
	m.mu.RUnlock()

	sort.Slice(result, func(left, right int) bool {
		return result[left].Root() < result[right].Root()
	})

	return result, nil
}

// Close removes a workspace and releases its retained source state.
func (m *Manager) Close(ctx context.Context, id ID) error {
	entry, owner := m.beginClose(id)
	if entry == nil {
		return nil
	}

	if owner {
		go m.finishClose(id, entry)
	}

	return entry.close.Wait(ctx)
}

// Clear removes and closes all retained workspace state.
func (m *Manager) Clear(ctx context.Context) error {
	m.mu.Lock()
	entries := make([]*workspaceEntry, 0, len(m.byID))
	owned := make([]*workspaceEntry, 0, len(m.byID))
	for _, entry := range m.byID {
		entries = append(entries, entry)
		if entry.state == workspaceEntryClosing {
			continue
		}

		if !entry.close.Begin() {
			panic("workspace: active entry close has already started")
		}
		entry.workspace.markClosing()
		entry.state = workspaceEntryClosing
		owned = append(owned, entry)
	}

	m.byRoot = make(map[string]ID)
	m.opening = make(map[string]*openOperation)
	m.generation++
	m.mu.Unlock()

	for _, entry := range owned {
		go m.finishClose(entry.workspace.ID(), entry)
	}

	var result error
	for _, entry := range entries {
		if err := entry.close.Wait(ctx); err != nil {
			result = errors.Join(result, err)
		}
	}

	return result
}

func (m *Manager) beginClose(id ID) (*workspaceEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := m.byID[id]
	if entry == nil {
		return nil, false
	}

	if entry.state == workspaceEntryClosing {
		return entry, false
	}

	if !entry.close.Begin() {
		panic("workspace: active entry close has already started")
	}
	entry.workspace.markClosing()
	entry.state = workspaceEntryClosing
	delete(m.byRoot, entry.workspace.Root())

	return entry, true
}

func (m *Manager) finishClose(id ID, entry *workspaceEntry) {
	m.mu.RLock()
	hooks := append([]CloseHook(nil), m.closeHooks...)
	m.mu.RUnlock()

	result := entry.workspace.stopWatcher()
	for _, hook := range hooks {
		if err := hook(context.Background(), id); err != nil {
			result = errors.Join(result, err)
		}
	}

	result = errors.Join(result, entry.workspace.close())
	entry.close.Finish(result)

	m.mu.Lock()
	if m.byID[id] == entry {
		delete(m.byID, id)
	}
	m.mu.Unlock()
}

func (m *Manager) find(root string) (*Workspace, *openOperation) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if id, ok := m.byRoot[root]; ok {
		return m.byID[id].workspace, nil
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
	watcher, err := m.newWatcher(operation.workspace.Root())
	if err == nil {
		err = watcher.AddDirectory(".")
	}

	var content workspaceContent
	if err == nil {
		content, err = m.loadWorkspace(ctx, operation.workspace.Root(), watcher.AddDirectory)
	}
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

	if err != nil {
		if engine != nil {
			err = errors.Join(err, engine.Close())
		}

		if watcher != nil {
			err = errors.Join(err, watcher.Close())
		}

		err = fmt.Errorf("%w: load %q: %w", ErrLoad, operation.workspace.Root(), err)
		operation.workspace.setFailed(err)

		m.finishOpen(operation, err)

		return nil, err
	}

	m.mu.Lock()
	if operation.generation != m.generation {
		m.mu.Unlock()

		err = fmt.Errorf("workspace manager was cleared during load")
		err = errors.Join(err, engine.Close(), watcher.Close())
		err = fmt.Errorf("%w: load %q: %w", ErrLoad, operation.workspace.Root(), err)
		operation.workspace.setFailed(err)
		m.finishOpen(operation, err)

		return nil, err
	}

	operation.workspace.setReady(content, engine, watcher)
	m.startWatcher(watcher, operation.workspace)
	m.byID[operation.workspace.ID()] = &workspaceEntry{
		workspace: operation.workspace,
		state:     workspaceEntryActive,
	}
	m.byRoot[operation.workspace.Root()] = operation.workspace.ID()
	delete(m.opening, operation.workspace.Root())
	close(operation.done)
	m.mu.Unlock()

	return operation.workspace, nil
}

func (m *Manager) finishOpen(operation *openOperation, err error) {
	m.mu.Lock()
	if m.opening[operation.workspace.Root()] == operation {
		delete(m.opening, operation.workspace.Root())
	}
	operation.err = err
	close(operation.done)
	m.mu.Unlock()
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
