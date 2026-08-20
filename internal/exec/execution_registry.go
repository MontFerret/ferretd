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
		execution *Execution
		state     registryState
	}

	executionGroup struct {
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

func (r *executionRegistry) add(execution *Execution) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries[execution.id] != nil {
		panic("execution: duplicate Execution ID")
	}

	entry := &executionEntry{execution: execution, state: registryStateActive}
	r.entries[execution.id] = entry

	group := r.bySession[execution.session]
	if group == nil {
		group = &executionGroup{
			state:   registryStateActive,
			entries: make(map[ExecutionID]*executionEntry),
		}
		r.bySession[execution.session] = group
	}
	if group.state != registryStateActive {
		panic("execution: closing Session accepted an Execution")
	}
	group.entries[execution.id] = entry
}

func (r *executionRegistry) active(id ExecutionID) *Execution {
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

	group := r.bySession[execution.session]
	if group == nil {
		return
	}

	if group.entries[execution.id] == entry {
		delete(group.entries, execution.id)
	}
	// Active empty groups are reused by later Executions from the same Session.
	// Parent close removes the group after the last retained child settles.
	if group.state == registryStateClosing && len(group.entries) == 0 {
		delete(r.bySession, execution.session)
	}
}
