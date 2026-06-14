// Package exec manages Ferret execution sessions.
package exec

// SessionManager is the future owner of execution sessions.
type SessionManager struct{}

// New creates an execution session manager.
func New() *SessionManager {
	return &SessionManager{}
}
