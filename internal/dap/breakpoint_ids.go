package dap

import (
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
)

type (
	// breakpointIDs owns protocol identities, not the debugger's active set.
	// Server.eventMu protects all access, including accepted commands whose
	// stop events have not yet been translated. A queued older stop must not
	// retire identities while a newer command can still report them.
	breakpointIDs struct {
		next    int
		pending int
		stable  map[breakpointKey]int
		active  map[apidebugger.BreakpointID]int
		retired map[apidebugger.BreakpointID]int
	}

	breakpointKey struct {
		sourceName string
		position   apisource.Position
	}
)

func newBreakpointIDs() *breakpointIDs {
	return &breakpointIDs{
		next:    1,
		stable:  make(map[breakpointKey]int),
		active:  make(map[apidebugger.BreakpointID]int),
		retired: make(map[apidebugger.BreakpointID]int),
	}
}

func (b *breakpointIDs) replace(source string, requests []apidebugger.BreakpointRequest, results []apidebugger.Breakpoint) {
	if b.pending != 0 {
		for native, stable := range b.active {
			b.retired[native] = stable
		}
	}

	clear(b.active)

	for index, result := range results {
		key := breakpointKey{sourceName: source, position: requests[index].Position}

		stable := b.stable[key]
		if stable == 0 {
			stable = b.next
			b.next++
			b.stable[key] = stable
		}

		b.active[result.ID] = stable
		delete(b.retired, result.ID)
	}
}

func (b *breakpointIDs) id(native apidebugger.BreakpointID) int {
	if stable := b.active[native]; stable != 0 {
		return stable
	}

	return b.retired[native]
}

func (b *breakpointIDs) commandStarted() {
	b.pending++
}

func (b *breakpointIDs) commandStopped() {
	if b.pending > 0 {
		b.pending--
	}

	if b.pending == 0 {
		clear(b.retired)
	}
}

func (b *breakpointIDs) finish() {
	b.pending = 0
	clear(b.active)
	clear(b.retired)
}
