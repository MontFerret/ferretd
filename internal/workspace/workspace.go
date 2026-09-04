package workspace

import (
	"sync"
	"sync/atomic"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	localsource "github.com/MontFerret/ferretd/internal/source"
)

type (
	// State identifies the current workspace lifecycle state.
	State uint8

	// Revision identifies one retained version of a workspace document.
	Revision uint64

	// Generation identifies one retained instance of a workspace document.
	// Assigned generations increase monotonically across deletion and same-path
	// recreation.
	Generation uint64

	// Workspace is the daemon-owned source state for a canonical root.
	Workspace struct {
		mu           sync.RWMutex
		mutationGate chan struct{}

		id                     ID
		root                   string
		state                  State
		failure                error
		files                  []File
		documents              map[string]Document
		order                  []string
		nextDocumentGeneration Generation
		watcher                *workspaceWatcher
		// closing is the lock-free admission barrier set before child cleanup;
		// state records resource teardown once the workspace lock is available.
		closing atomic.Bool
	}

	// SourceSnapshot identifies the immutable workspace document compiled into a Plan.
	SourceSnapshot struct {
		Workspace    ID
		RelativePath string
		URI          localsource.URI
		// Revision advances monotonically while one retained document identity
		// receives changed source state.
		Revision Revision
	}
)

const (
	// StateOpening identifies a workspace whose initial source load is running.
	StateOpening State = iota + 1
	// StateReady identifies a successfully loaded workspace.
	StateReady
	// StateFailed identifies a workspace whose initial source load failed.
	StateFailed
	// StateClosing identifies a workspace whose child resources are being released.
	StateClosing
	// StateClosed identifies a workspace whose retained source state was released.
	StateClosed
)

func newWorkspace(id ID, root string) *Workspace {
	result := &Workspace{
		id:           id,
		root:         root,
		state:        StateOpening,
		mutationGate: make(chan struct{}, 1),
	}
	result.mutationGate <- struct{}{}

	return result
}

// ID returns the workspace's opaque identifier.
func (w *Workspace) ID() ID {
	return w.id
}

// Root returns the canonical workspace root.
func (w *Workspace) Root() string {
	return w.root
}

// State returns the workspace's current lifecycle state.
func (w *Workspace) State() State {
	w.mu.RLock()
	state := w.state
	w.mu.RUnlock()
	if w.closing.Load() && state != StateClosed && state != StateFailed {
		return StateClosing
	}

	return state
}

// Failure returns the retained cause of a failed initial load.
func (w *Workspace) Failure() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.failure
}

// Files returns deterministic value snapshots of discovered source files.
func (w *Workspace) Files() []File {
	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]File, len(w.files))
	copy(result, w.files)

	return result
}

// Documents returns deterministic daemon-owned document snapshots.
func (w *Workspace) Documents() []Document {
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
	key, ok := normalizeDocumentPath(relativePath)
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
	w.mu.RLock()
	defer w.mu.RUnlock()

	var result []*ferretdiagnostics.Diagnostic
	for _, relativePath := range w.order {
		result = append(result, cloneDiagnostics(w.documents[relativePath].diagnostics)...)
	}

	return result
}

func (w *Workspace) setReady(
	content workspaceContent,
	watcher *workspaceWatcher,
) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if content.documents == nil {
		content.documents = make(map[string]Document)
	}

	for _, relativePath := range content.order {
		document := content.documents[relativePath]
		w.nextDocumentGeneration++
		content.documents[relativePath] = document.withGeneration(w.nextDocumentGeneration)
	}

	w.files = content.files
	w.documents = content.documents
	w.order = content.order
	w.watcher = watcher
	w.failure = nil
	w.state = StateReady
}

func (w *Workspace) setFailed(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.files = nil
	w.documents = nil
	w.order = nil
	w.watcher = nil
	w.failure = err
	w.state = StateFailed
}

func (w *Workspace) markClosing() {
	w.closing.Store(true)
}

func (w *Workspace) stopWatcher() error {
	w.mu.RLock()
	watcher := w.watcher
	w.mu.RUnlock()
	if watcher == nil {
		return nil
	}

	return watcher.Close()
}

func (w *Workspace) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.state == StateClosed {
		return nil
	}

	w.watcher = nil
	w.files = nil
	w.documents = nil
	w.order = nil
	w.nextDocumentGeneration = 0
	w.failure = nil
	w.state = StateClosed

	return nil
}
