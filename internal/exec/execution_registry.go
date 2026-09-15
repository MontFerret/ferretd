package exec

import "sync"

type (
	// executionRegistry owns Execution reachability, lifecycle state, and Session
	// membership. No caller holds a sessionRegistry lock while entering it.
	executionRegistry struct {
		mu sync.RWMutex

		entries   map[ExecutionID]*executionEntry
		bySession map[SessionID]*executionGroup
	}

	executionEntry struct {
		execution *execution
		state     registryState
	}

	executionGroup struct {
		parent  *session
		state   registryState
		entries map[ExecutionID]*executionEntry
	}

	executionClose struct {
		entry *executionEntry
		owner bool
	}
)

func newExecutionRegistry() *executionRegistry {
	return &executionRegistry{
		entries:   make(map[ExecutionID]*executionEntry),
		bySession: make(map[SessionID]*executionGroup),
	}
}

func (r *executionRegistry) add(execution *execution, parent *session) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries[execution.id] != nil {
		panic("execution: duplicate Execution ID")
	}

	entry := &executionEntry{execution: execution, state: registryStateActive}
	r.entries[execution.id] = entry

	group := r.bySession[execution.runtime.target.sessionID]
	if group == nil {
		group = &executionGroup{
			parent:  parent,
			state:   registryStateActive,
			entries: make(map[ExecutionID]*executionEntry),
		}
		r.bySession[execution.runtime.target.sessionID] = group
	}

	if group.state != registryStateActive {
		panic("execution: closing Session accepted an Execution")
	}

	group.entries[execution.id] = entry
}

func (r *executionRegistry) active(id ExecutionID) *execution {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry := r.entries[id]
	if entry == nil || entry.state != registryStateActive {
		return nil
	}

	return entry.execution
}

func (r *executionRegistry) beginClose(id ExecutionID) executionClose {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.entries[id]
	if entry == nil {
		return executionClose{}
	}

	return r.beginCloseLocked(entry)
}

func (r *executionRegistry) beginSessionClose(id SessionID) []executionClose {
	r.mu.Lock()
	defer r.mu.Unlock()

	group := r.bySession[id]
	if group == nil {
		return nil
	}

	group.state = registryStateClosing
	result := make([]executionClose, 0, len(group.entries))

	for _, entry := range group.entries {
		result = append(result, r.beginCloseLocked(entry))
	}

	if len(group.entries) == 0 {
		delete(r.bySession, id)
	}

	return result
}

func (r *executionRegistry) beginCloseLocked(entry *executionEntry) executionClose {
	if entry.state == registryStateClosing {
		return executionClose{entry: entry}
	}

	if !entry.execution.beginClose() {
		panic("execution: active Execution close has already started")
	}

	entry.state = registryStateClosing

	return executionClose{entry: entry, owner: true}
}

func (r *executionRegistry) finishClose(entry *executionEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	execution := entry.execution
	if r.entries[execution.id] == entry {
		delete(r.entries, execution.id)
	}

	group := r.bySession[execution.runtime.target.sessionID]
	if group == nil {
		return
	}

	// A parent may have stopped admission while its close worker is still
	// waiting for creators. Retain the completed child until that worker has
	// collected its cleanup result, even after the child leaves public lookup.
	if group.state == registryStateActive && group.parent.closeStarted() {
		return
	}

	if group.entries[execution.id] == entry {
		delete(group.entries, execution.id)
	}

	// Active empty groups are reused by later Executions from the same Session.
	// Parent close removes the group after the last retained child settles.
	if group.state == registryStateClosing && len(group.entries) == 0 {
		delete(r.bySession, execution.runtime.target.sessionID)
	}
}

func (r *executionRegistry) finishSessionClose(id SessionID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Parent cleanup has observed every retained child, including children that
	// completed between admission closing and beginSessionClose.
	delete(r.bySession, id)
}
