package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/ferret/v2"
	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	localsource "github.com/MontFerret/ferretd/internal/source"
)

type (
	// State identifies the current workspace lifecycle state.
	State uint8

	// Revision identifies one retained version of a workspace document.
	Revision uint64

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
		nextDocumentGeneration uint64
		engine                 *ferret.Engine
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

// CompileDocument compiles an immutable document returned by RefreshDocument.
// A later workspace refresh cannot change the source selected for this compile.
func (w *Workspace) CompileDocument(ctx context.Context, document Document) (Compilation, error) {
	if err := ctx.Err(); err != nil {
		return Compilation{}, err
	}

	if w.closing.Load() {
		return Compilation{}, ErrClosed
	}

	key, ok := normalizeDocumentPath(document.File().RelativePath)
	if !ok {
		return Compilation{}, ErrDocumentNotFound
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.closing.Load() || w.state != StateReady || w.engine == nil {
		return Compilation{}, ErrClosed
	}

	known, ok := w.documents[key]
	if !ok || known.File() != document.File() {
		return Compilation{}, ErrDocumentNotFound
	}

	return w.compileDocumentLocked(ctx, document)
}

// CompileDebugSnapshot compiles retained Session source with Ferret debug metadata.
// The source text and revision are owned by the Session and need not match the
// workspace's current document revision.
func (w *Workspace) CompileDebugSnapshot(
	ctx context.Context,
	snapshot SourceSnapshot,
	content string,
) (Compilation, error) {
	if err := ctx.Err(); err != nil {
		return Compilation{}, err
	}

	if w.closing.Load() || snapshot.Workspace != w.id {
		return Compilation{}, ErrClosed
	}

	key, ok := normalizeDocumentPath(snapshot.RelativePath)
	if !ok {
		return Compilation{}, ErrDocumentNotFound
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.closing.Load() || w.state != StateReady || w.engine == nil {
		return Compilation{}, ErrClosed
	}

	absolute := filepath.Join(w.root, filepath.FromSlash(key))
	uri, err := localsource.URIFromPath(absolute)
	if err != nil || uri != snapshot.URI {
		return Compilation{}, ErrDocumentNotFound
	}

	plan, err := w.engine.CompileDebug(ctx, ferretsource.New(absolute, content))
	if err != nil {
		return Compilation{Source: snapshot}, err
	}

	if err := ctx.Err(); err != nil {
		return Compilation{Source: snapshot, Plan: plan}, err
	}

	return Compilation{Source: snapshot, Plan: plan}, nil
}

func (w *Workspace) compileDocumentLocked(
	ctx context.Context,
	document Document,
) (Compilation, error) {
	snapshot := SourceSnapshot{
		Workspace:    w.id,
		RelativePath: document.File().RelativePath,
		URI:          document.File().URI,
		Revision:     document.Revision(),
	}

	if !document.Loaded() {
		return Compilation{Source: snapshot}, fmt.Errorf(
			"%w: %s",
			ErrDocumentUnavailable,
			document.File().RelativePath,
		)
	}

	plan, err := w.engine.Compile(ctx, document.Source())
	if err != nil {
		return Compilation{Source: snapshot}, err
	}

	if err := ctx.Err(); err != nil {
		return Compilation{Source: snapshot, Plan: plan}, err
	}

	return Compilation{Source: snapshot, Plan: plan}, nil
}

func (w *Workspace) setReady(
	content workspaceContent,
	engine *ferret.Engine,
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
	w.engine = engine
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
	w.engine = nil
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

	if w.state == StateClosed {
		w.mu.Unlock()

		return nil
	}

	engine := w.engine
	w.engine = nil
	w.watcher = nil
	w.state = StateClosing
	w.mu.Unlock()

	var result error
	if engine != nil {
		if err := engine.Close(); err != nil {
			result = errors.Join(ErrLoad, fmt.Errorf("close workspace engine: %w", err))
		}
	}

	w.mu.Lock()
	w.files = nil
	w.documents = nil
	w.order = nil
	w.nextDocumentGeneration = 0
	w.failure = nil
	w.state = StateClosed
	w.mu.Unlock()

	return result
}
