package dap

import (
	"slices"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/debug"
)

type (
	// initialStop retains only the bindings needed to interpret native entry.
	// Server.eventMu protects it. Once start is accepted, live replacements
	// must not retroactively change the initial suspension's breakpoint cause.
	initialStop struct {
		bindings []entryBinding
		frozen   bool
	}

	entryBinding struct {
		id       apidebugger.BreakpointID
		location apisource.Location
	}
)

func (s *initialStop) replace(breakpoints []apidebugger.Breakpoint) {
	if s.frozen {
		return
	}

	s.bindings = s.bindings[:0]

	for _, breakpoint := range breakpoints {
		if breakpoint.Bound {
			s.bindings = append(s.bindings, entryBinding{id: breakpoint.ID, location: breakpoint.Location.Location})
		}
	}
}

func (s *initialStop) resolve(snapshot debug.SessionSnapshot, stopOnEntry bool, ids *breakpointIDs) (debug.SessionSnapshot, bool) {
	if snapshot.Reason != apidebugger.ReasonEntry {
		return snapshot, false
	}

	// Native Start reports entry without testing breakpoints, and Continue skips
	// that point once. Use its resolved bindings, including the byte column, to
	// report the breakpoint before deciding whether entry can be suppressed.
	if len(snapshot.HitBreakpointIDs) == 0 {
		seen := make(map[int]bool)

		for _, binding := range s.bindings {
			id := ids.id(binding.id)
			if binding.location == snapshot.Location && !seen[id] {
				snapshot.HitBreakpointIDs = append(snapshot.HitBreakpointIDs, binding.id)
				seen[id] = true
			}
		}

		slices.Sort(snapshot.HitBreakpointIDs)
	}

	if len(snapshot.HitBreakpointIDs) != 0 {
		snapshot.Reason = apidebugger.ReasonBreakpoint

		return snapshot, false
	}

	return snapshot, !stopOnEntry
}
