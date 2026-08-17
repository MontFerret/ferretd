package dap

import (
	"sync"

	"github.com/MontFerret/ferretd/internal/exec"
)

type handleTable struct {
	mu sync.Mutex

	next      int
	frames    map[int]int
	scopes    map[int][]exec.DebugVariable
	variables map[int]exec.DebugValueReference
}

func newHandleTable() *handleTable {
	return &handleTable{
		next:      1,
		frames:    make(map[int]int),
		scopes:    make(map[int][]exec.DebugVariable),
		variables: make(map[int]exec.DebugValueReference),
	}
}

func (t *handleTable) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.next = 1
	clear(t.frames)
	clear(t.scopes)
	clear(t.variables)
}

func (t *handleTable) Frame(index int) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	handle := t.allocateLocked()
	t.frames[handle] = index

	return handle
}

func (t *handleTable) FrameIndex(handle int) (int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	index, ok := t.frames[handle]

	return index, ok
}

func (t *handleTable) Scope(variables []exec.DebugVariable) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	handle := t.allocateLocked()
	t.scopes[handle] = append([]exec.DebugVariable(nil), variables...)

	return handle
}

func (t *handleTable) ScopeVariables(handle int) ([]exec.DebugVariable, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	variables, ok := t.scopes[handle]

	return append([]exec.DebugVariable(nil), variables...), ok
}

func (t *handleTable) Variable(reference exec.DebugValueReference) int {
	if reference == 0 {
		return 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	handle := t.allocateLocked()
	t.variables[handle] = reference

	return handle
}

func (t *handleTable) VariableReference(handle int) (exec.DebugValueReference, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	reference, ok := t.variables[handle]

	return reference, ok
}

func (t *handleTable) allocateLocked() int {
	handle := t.next
	t.next++

	return handle
}
