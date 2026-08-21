package exec

import "github.com/MontFerret/ferretd/internal/workspace"

// SessionSnapshot is an immutable view of a daemon Session.
type SessionSnapshot struct {
	ID         SessionID
	Source     workspace.SourceSnapshot
	Parameters []string
}
