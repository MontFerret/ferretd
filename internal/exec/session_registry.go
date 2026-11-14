package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferretd/internal/lifecycle"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type (
	registryState uint8

	// sessionRegistry owns Session reachability, lifecycle state, workspace
	// membership, and service-wide creation admission. Its lock precedes the
	// group and Session gates. Installing a cancellation fence also reads
	// workspace membership; workspace close hooks run outside that manager's lock.
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

	sessionAcquisition struct {
		operation *construction
		published *sessionEntry
		cancelled bool
		settled   lifecycle.CloseOperation
	}

	sessionCreation struct {
		acquisitionID string
		acquisition   *sessionAcquisition
		workspace     workspace.ID
		group         *workspaceGroup
		operation     *construction
	}

	runtimeCreation struct {
		parent    *session
		operation *construction
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

func (r *sessionRegistry) beginCreate(id workspace.ID, operation *construction, acquisitionID string) (sessionCreation, error) {
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

	if prior := group.acquisitions[acquisitionID]; prior != nil {
		if prior.cancelled {
			return sessionCreation{}, context.Canceled
		}

		return sessionCreation{}, ErrDuplicateAcquisition
	}

	if !group.beginCreate(operation) {
		return sessionCreation{}, workspace.ErrClosed
	}

	creation := sessionCreation{workspace: id, group: group, operation: operation, acquisitionID: acquisitionID}
	if acquisitionID != "" {
		creation.acquisition = group.newAcquisition(acquisitionID)
		creation.acquisition.operation = operation
	}

	return creation, nil
}

func (r *sessionRegistry) finishCreate(creation sessionCreation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if a := creation.acquisition; a != nil && a.operation != nil {
		a.operation = nil
		a.settled.Finish(creation.operation.cleanupErr)

		if creation.operation.cleanupErr != nil {
			a.cancelled = true
		}

		if !a.cancelled {
			delete(creation.group.acquisitions, creation.acquisitionID)
		}
	}

	discard, closedErr := creation.group.finishCreate(creation.operation)
	if discard && len(creation.group.acquisitions) == 0 && r.groups[creation.workspace] == creation.group {
		delete(r.groups, creation.workspace)
	}

	return closedErr
}

func (r *sessionRegistry) commitCreate(
	ctx context.Context,
	creation sessionCreation,
	session *session,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || r.groups[creation.workspace] != creation.group || !creation.group.accepting() {
		return errors.Join(workspace.ErrClosed, constructionError(ctx))
	}

	if a := creation.acquisition; a != nil && a.cancelled {
		return context.Canceled
	}

	if err := constructionError(ctx); err != nil {
		return err
	}

	entry := &sessionEntry{session: session, state: registryStateActive}
	if !creation.group.add(entry) {
		return workspace.ErrClosed
	}

	if a := creation.acquisition; a != nil {
		a.published = entry
		a.operation = nil
		session.acquisitionID = creation.acquisitionID
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

func (r *sessionRegistry) beginRuntimeCreate(id SessionID, operation *construction) (runtimeCreation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return runtimeCreation{}, ErrClosed
	}

	entry := r.entries[id]
	if entry == nil || entry.state != registryStateActive {
		return runtimeCreation{}, ErrSessionNotFound
	}

	if !entry.session.beginRuntimeCreate(operation) {
		panic("execution: active Session rejected runtime creation")
	}

	return runtimeCreation{parent: entry.session, operation: operation}, nil
}

func (c runtimeCreation) session() *session {
	return c.parent
}

func (c runtimeCreation) finish() error {
	return c.parent.finishRuntimeCreate(c.operation)
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

func (r *sessionRegistry) finishClose(entry *sessionEntry, result error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// A waiter that sees ordinary close succeed may immediately reuse the key.
	defer entry.session.finishClose(result)

	if r.entries[entry.session.id] == entry {
		delete(r.entries, entry.session.id)
	}

	workspaceID := entry.session.source.Workspace

	group := r.groups[workspaceID]
	if group == nil {
		return
	}

	if a := group.acquisitions[entry.session.acquisitionID]; a != nil {
		a.published = nil
		a.settled.Finish(result)

		if result != nil {
			a.cancelled = true
		}

		if !a.cancelled {
			delete(group.acquisitions, entry.session.acquisitionID)
		}
	}

	discard := group.removeCompleted(entry)
	if discard && len(group.acquisitions) == 0 && r.groups[workspaceID] == group {
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
