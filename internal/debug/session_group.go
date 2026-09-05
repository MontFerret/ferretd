package debug

import "github.com/MontFerret/ferretd/internal/lifecycle"

type sessionGroup struct {
	// Manager.mu is acquired before gate when both are needed. Gate never
	// calls back into the Manager, so the lock order cannot reverse.
	gate     lifecycle.Gate
	sessions map[SessionID]*session
}
