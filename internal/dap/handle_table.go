package dap

import (
	"sync"

	apidebugger "github.com/MontFerret/api/debugger"
)

type (
	handleStatus uint8
	handleKind   uint8

	handleInvalidation struct {
		frames    int
		scopes    int
		variables int
		stale     int
	}

	handleTable struct {
		mu sync.Mutex

		next      int
		frames    map[int]int
		scopes    map[int][]apidebugger.Variable
		variables map[int]apidebugger.ValueReference
		stale     map[int]handleKind
	}
)

const (
	handleInvalid handleStatus = iota
	handleCurrent
	handleStale
)

const (
	frameHandle handleKind = iota + 1
	scopeHandle
	variableHandle
)

func newHandleTable() *handleTable {
	return &handleTable{
		next:      1,
		frames:    make(map[int]int),
		scopes:    make(map[int][]apidebugger.Variable),
		variables: make(map[int]apidebugger.ValueReference),
		stale:     make(map[int]handleKind),
	}
}

func (t *handleTable) Invalidate() handleInvalidation {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := handleInvalidation{
		frames:    len(t.frames),
		scopes:    len(t.scopes),
		variables: len(t.variables),
	}
	if result.frames+result.scopes+result.variables > 0 {
		clear(t.stale)
		for handle := range t.frames {
			t.stale[handle] = frameHandle
		}
		for handle := range t.scopes {
			t.stale[handle] = scopeHandle
		}
		for handle := range t.variables {
			t.stale[handle] = variableHandle
		}
	}

	clear(t.frames)
	clear(t.scopes)
	clear(t.variables)
	result.stale = len(t.stale)

	return result
}

func (t *handleTable) Frame(index int) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	handle := t.allocateLocked()
	t.frames[handle] = index

	return handle
}

func (t *handleTable) FrameIndex(handle int) (int, handleStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()

	index, ok := t.frames[handle]
	if ok {
		return index, handleCurrent
	}
	if t.stale[handle] == frameHandle {
		return 0, handleStale
	}

	return 0, handleInvalid
}

func (t *handleTable) Scope(variables []apidebugger.Variable) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	handle := t.allocateLocked()
	t.scopes[handle] = append([]apidebugger.Variable(nil), variables...)

	return handle
}

func (t *handleTable) ScopeVariables(handle int) ([]apidebugger.Variable, handleStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()

	variables, ok := t.scopes[handle]
	if ok {
		return append([]apidebugger.Variable(nil), variables...), handleCurrent
	}
	if t.stale[handle] == scopeHandle {
		return nil, handleStale
	}

	return nil, handleInvalid
}

func (t *handleTable) Variable(reference apidebugger.ValueReference) int {
	if reference == 0 {
		return 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	handle := t.allocateLocked()
	t.variables[handle] = reference

	return handle
}

func (t *handleTable) VariableReference(
	handle int,
) (apidebugger.ValueReference, handleStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()

	reference, ok := t.variables[handle]
	if ok {
		return reference, handleCurrent
	}
	if t.stale[handle] == variableHandle {
		return 0, handleStale
	}

	return 0, handleInvalid
}

func (t *handleTable) allocateLocked() int {
	handle := t.next
	t.next++

	return handle
}

func (s *Server) invalidateHandles(cause string) {
	invalidated := s.handles.Invalidate()
	s.logger.Debug().
		Str("cause", cause).
		Int("frames", invalidated.frames).
		Int("scopes", invalidated.scopes).
		Int("variables", invalidated.variables).
		Int("stale_handles", invalidated.stale).
		Msg("DAP handles invalidated")
}
