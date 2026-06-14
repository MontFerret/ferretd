// Package debug manages Ferret debug sessions.
package debug

// SessionManager is the future owner of debug sessions.
type SessionManager struct{}

// New creates a debug session manager.
func New() *SessionManager {
	return &SessionManager{}
}
