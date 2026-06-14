// Package workspace manages the workspaces known to ferretd.
package workspace

// Manager is the future owner of workspace state.
type Manager struct{}

// New creates a workspace manager.
func New() *Manager {
	return &Manager{}
}
