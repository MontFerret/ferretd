package workspace

// ID is an opaque workspace identifier.
type ID string

// Workspace is an immutable snapshot of daemon-owned workspace state.
type Workspace struct {
	ID   ID
	Root string
}
