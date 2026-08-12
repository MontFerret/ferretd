package workspace

import (
	"sync"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
)

type (
	// ID is an opaque workspace identifier.
	ID string

	// State identifies the current workspace lifecycle state.
	State uint8

	// Workspace is the daemon-owned source state for a canonical root.
	Workspace struct {
		mu sync.RWMutex

		id        ID
		root      string
		state     State
		failure   error
		files     []File
		documents map[string]Document
		order     []string
	}
)

const (
	// StateOpening identifies a workspace whose initial source load is running.
	StateOpening State = iota
	// StateReady identifies a successfully loaded workspace.
	StateReady
	// StateFailed identifies a workspace whose initial source load failed.
	StateFailed
	// StateClosed identifies a workspace whose retained source state was released.
	StateClosed
)

func newWorkspace(id ID, root string) *Workspace {
	return &Workspace{
		id:    id,
		root:  root,
		state: StateOpening,
	}
}

// ID returns the workspace's opaque identifier.
func (w *Workspace) ID() ID {
	if w == nil {
		return ""
	}

	return w.id
}

// Root returns the canonical workspace root.
func (w *Workspace) Root() string {
	if w == nil {
		return ""
	}

	return w.root
}

// State returns the workspace's current lifecycle state.
func (w *Workspace) State() State {
	if w == nil {
		return StateClosed
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.state
}

// Failure returns the retained cause of a failed initial load.
func (w *Workspace) Failure() error {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.failure
}

// Files returns deterministic value snapshots of discovered source files.
func (w *Workspace) Files() []File {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]File, len(w.files))
	copy(result, w.files)

	return result
}

// Documents returns deterministic daemon-owned document snapshots.
func (w *Workspace) Documents() []Document {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]Document, 0, len(w.order))
	for _, relativePath := range w.order {
		result = append(result, w.documents[relativePath])
	}

	return result
}

// Document returns a document by its workspace-relative path.
func (w *Workspace) Document(relativePath string) (Document, bool) {
	if w == nil {
		return Document{}, false
	}

	key, ok := documentKey(relativePath)
	if !ok {
		return Document{}, false
	}

	w.mu.RLock()
	document, ok := w.documents[key]
	w.mu.RUnlock()

	return document, ok
}

// Diagnostics returns copies of all document diagnostics in document order.
func (w *Workspace) Diagnostics() []*ferretdiagnostics.Diagnostic {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	var result []*ferretdiagnostics.Diagnostic
	for _, relativePath := range w.order {
		result = append(result, cloneDiagnostics(w.documents[relativePath].diagnostics)...)
	}

	return result
}

func (w *Workspace) setReady(content workspaceContent) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.files = content.files
	w.documents = content.documents
	w.order = content.order
	w.failure = nil
	w.state = StateReady
}

func (w *Workspace) setFailed(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.files = nil
	w.documents = nil
	w.order = nil
	w.failure = err
	w.state = StateFailed
}

func (w *Workspace) close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.files = nil
	w.documents = nil
	w.order = nil
	w.failure = nil
	w.state = StateClosed
}
