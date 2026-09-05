package debug

import apidebugger "github.com/MontFerret/api/debugger"

type (
	// ScopeKind identifies a transport-neutral debugger scope.
	ScopeKind uint8

	// Scope groups variables visible in one frame for adapter presentation.
	Scope struct {
		Kind      ScopeKind
		Name      string
		Variables []apidebugger.Variable
	}
)

const (
	// ScopeLocals identifies variables local to the selected frame.
	ScopeLocals ScopeKind = iota + 1
	// ScopeParameters identifies caller-supplied execution parameters.
	ScopeParameters
)
