package debug

type (
	// ScopeKind identifies a transport-neutral debugger scope.
	ScopeKind uint8

	// ValueReference identifies an expandable value in one paused state.
	ValueReference uint64

	// BreakpointID identifies a resolved breakpoint and corresponding hit.
	BreakpointID uint64

	// Location identifies a 1-based source position.
	Location struct {
		File   string
		Line   int
		Column int
	}

	// BreakpointLocation identifies a requested 1-based source position.
	BreakpointLocation struct {
		Line   int
		Column int
	}

	// Breakpoint describes a requested and resolved source breakpoint.
	Breakpoint struct {
		ID              BreakpointID
		File            string
		RequestedLine   int
		RequestedColumn int
		Line            int
		Column          int
		Verified        bool
	}

	// Frame describes one paused frame. Index zero is the current frame.
	Frame struct {
		Index    int
		Name     string
		Location Location
	}

	// Scope groups variables visible in one frame.
	Scope struct {
		Kind      ScopeKind
		Name      string
		Variables []Variable
	}

	// Value is a bounded, debugger-safe value representation.
	Value struct {
		Type      string
		Display   string
		Reference ValueReference
	}

	// Variable is one visible local or parameter.
	Variable struct {
		Name    string
		Value   Value
		Mutable bool
	}
)

const (
	ScopeLocals ScopeKind = iota + 1
	ScopeParameters
)
