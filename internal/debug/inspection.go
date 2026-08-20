package debug

// ScopeKind identifies a transport-neutral debugger scope.
type ScopeKind uint8

// ValueReference identifies an expandable value in one paused state.
type ValueReference uint64

// Location identifies a 1-based source position.
type Location struct {
	File   string
	Line   int
	Column int
}

// BreakpointLocation identifies a requested 1-based source position.
type BreakpointLocation struct {
	Line   int
	Column int
}

// Breakpoint describes a requested and resolved source breakpoint.
type Breakpoint struct {
	ID              uint64
	File            string
	RequestedLine   int
	RequestedColumn int
	Line            int
	Column          int
	Verified        bool
}

// Frame describes one paused frame. Index zero is the current frame.
type Frame struct {
	Index    int
	Name     string
	Location Location
}

// Scope groups variables visible in one frame.
type Scope struct {
	Kind      ScopeKind
	Name      string
	Variables []Variable
}

// Value is a bounded, debugger-safe value representation.
type Value struct {
	Type      string
	Display   string
	Reference ValueReference
}

// Variable is one visible local or parameter.
type Variable struct {
	Name    string
	Value   Value
	Mutable bool
}

const (
	ScopeLocals ScopeKind = iota + 1
	ScopeParameters
)
