package exec

import (
	"context"
	"sync"

	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	registryState uint8

	// sessionRegistry owns Session reachability, lifecycle state, workspace
	// membership, and service-wide creation admission. Its lock may nest only into
	// a workspaceGroup or Session lifecycle gate.
	sessionRegistry struct {
		mu sync.RWMutex

		entries map[SessionID]*sessionEntry
		groups  map[workspace.ID]*workspaceGroup
		closed  bool
	}

	sessionEntry struct {
		session *session
		state   registryState
	}

	sessionCreation struct {
		workspace workspace.ID
		group     *workspaceGroup
	}

	runtimeCreation struct {
		parent *session
	}

	workspaceClose struct {
		id    workspace.ID
		group *workspaceGroup
		owner bool
	}
)

const (
	registryStateActive registryState = iota + 1
	registryStateClosing
)

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		entries: make(map[SessionID]*sessionEntry),
		groups:  make(map[workspace.ID]*workspaceGroup),
	}
}

func (r *sessionRegistry) beginCreate(id workspace.ID) (sessionCreation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return sessionCreation{}, ErrClosed
	}

	group := r.groups[id]
	if group == nil {
		group = newWorkspaceGroup()
		r.groups[id] = group
	}

	if !group.beginCreate() {
		return sessionCreation{}, workspace.ErrClosed
	}

	return sessionCreation{workspace: id, group: group}, nil
}

func (r *sessionRegistry) finishCreate(creation sessionCreation) {
	r.mu.Lock()
	defer r.mu.Unlock()

	discard := creation.group.finishCreate()
	if discard && r.groups[creation.workspace] == creation.group {
		delete(r.groups, creation.workspace)
	}
}

func (r *sessionRegistry) commitCreate(
	ctx context.Context,
	creation sessionCreation,
	session *session,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || r.groups[creation.workspace] != creation.group || !creation.group.accepting() {
		return workspace.ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	entry := &sessionEntry{session: session, state: registryStateActive}
	if !creation.group.add(entry) {
		return workspace.ErrClosed
	}

	r.entries[session.id] = entry

	return nil
}

func (r *sessionRegistry) active(id SessionID) *session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry := r.entries[id]
	if entry == nil || entry.state != registryStateActive {
		return nil
	}

	return entry.session
}

func (r *sessionRegistry) beginRuntimeCreate(id SessionID) (runtimeCreation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return runtimeCreation{}, ErrClosed
	}

	entry := r.entries[id]
	if entry == nil || entry.state != registryStateActive {
		return runtimeCreation{}, ErrSessionNotFound
	}

	if !entry.session.beginRuntimeCreate() {
		panic("execution: active Session rejected runtime creation")
	}

	return runtimeCreation{parent: entry.session}, nil
}

func (c runtimeCreation) session() *session {
	return c.parent
}

func (c runtimeCreation) finish() {
	c.parent.finishRuntimeCreate()
}

func (r *sessionRegistry) beginClose(id SessionID, retained *sessionEntry) (*sessionEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[id]
	if entry == nil {
		// A workspace group retains its captured child so parent teardown can
		// join a close result after the global lookup stops retaining it.
		entry = retained
	}
	if entry == nil {
		return nil, false
	}

	if entry.state == registryStateClosing {
		return entry, false
	}

	if !entry.session.beginClose() {
		panic("execution: active Session close has already started")
	}
	entry.state = registryStateClosing

	return entry, true
}

func (r *sessionRegistry) finishClose(entry *sessionEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries[entry.session.id] == entry {
		delete(r.entries, entry.session.id)
	}

	workspaceID := entry.session.source.Workspace
	group := r.groups[workspaceID]
	if group == nil {
		return
	}

	discard := group.removeCompleted(entry)
	if discard && r.groups[workspaceID] == group {
		delete(r.groups, workspaceID)
	}
}

func (r *sessionRegistry) beginWorkspaceClose(id workspace.ID) workspaceClose {
	r.mu.Lock()
	defer r.mu.Unlock()

	group := r.groups[id]
	if group == nil {
		group = newWorkspaceGroup()
		r.groups[id] = group
	}

	return workspaceClose{id: id, group: group, owner: group.beginClose()}
}

func (r *sessionRegistry) beginShutdown() []workspaceClose {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
	result := make([]workspaceClose, 0, len(r.groups))
	for id, group := range r.groups {
		result = append(result, workspaceClose{id: id, group: group, owner: group.beginClose()})
	}

	return result
}

func (r *sessionRegistry) finishWorkspaceClose(closing workspaceClose, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Publish registry removal before waking close waiters. Holding the registry
	// lock across both steps prevents a waiter from observing the stale group or
	// a new creator from interleaving between removal and completion.
	if r.groups[closing.id] == closing.group {
		delete(r.groups, closing.id)
	}

	closing.group.finishClose(err)
}
