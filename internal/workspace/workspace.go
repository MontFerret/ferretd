package workspace

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/MontFerret/ferret/v2"
	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	localsource "github.com/MontFerret/ferretd/internal/source"
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
		engine    *ferret.Engine
		closing   atomic.Bool
	}

	// SourceSnapshot identifies the immutable workspace document compiled into a Plan.
	SourceSnapshot struct {
		Workspace    ID
		RelativePath string
		URI          localsource.URI
		Revision     uint64
	}

	// Compilation owns a Ferret Plan and the source snapshot that produced it.
	Compilation struct {
		Plan   *ferret.Plan
		Source SourceSnapshot
	}
)

const (
	// StateOpening identifies a workspace whose initial source load is running.
	StateOpening State = iota
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
	state := w.state
	w.mu.RUnlock()
	if w.closing.Load() && state != StateClosed && state != StateFailed {
		return StateClosing
	}

	return state
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

// Compile compiles one retained document through the workspace-owned Ferret engine.
func (w *Workspace) Compile(ctx context.Context, relativePath string) (Compilation, error) {
	return w.compile(ctx, relativePath, false)
}

// CompileDebug compiles one retained document with Ferret debug metadata.
func (w *Workspace) CompileDebug(ctx context.Context, relativePath string) (Compilation, error) {
	return w.compile(ctx, relativePath, true)
}

func (w *Workspace) compile(ctx context.Context, relativePath string, debug bool) (Compilation, error) {
	if err := ctx.Err(); err != nil {
		return Compilation{}, err
	}

	if w.closing.Load() {
		return Compilation{}, ErrClosed
	}

	key, ok := documentKey(relativePath)
	if !ok {
		return Compilation{}, ErrDocumentNotFound
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.closing.Load() || w.state != StateReady || w.engine == nil {
		return Compilation{}, ErrClosed
	}

	document, ok := w.documents[key]
	if !ok {
		return Compilation{}, ErrDocumentNotFound
	}

	snapshot := SourceSnapshot{
		Workspace:    w.id,
		RelativePath: document.File().RelativePath,
		URI:          document.File().URI,
		Revision:     document.Revision(),
	}

	if !document.Loaded() {
		return Compilation{Source: snapshot}, fmt.Errorf("%w: %s", ErrDocumentUnavailable, key)
	}

	var plan *ferret.Plan
	var err error
	if debug {
		plan, err = w.engine.CompileDebug(ctx, document.Source())
	} else {
		plan, err = w.engine.Compile(ctx, document.Source())
	}
	if err != nil {
		return Compilation{Source: snapshot}, err
	}

	if err := ctx.Err(); err != nil {
		return Compilation{Source: snapshot, Plan: plan}, err
	}

	return Compilation{Source: snapshot, Plan: plan}, nil
}

func (w *Workspace) setReady(content workspaceContent, engine *ferret.Engine) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.files = content.files
	w.documents = content.documents
	w.order = content.order
	w.engine = engine
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
	w.failure = err
	w.state = StateFailed
}

func (w *Workspace) markClosing() {
	w.closing.Store(true)
}

func (w *Workspace) close() error {
	w.mu.Lock()

	if w.state == StateClosed {
		w.mu.Unlock()

		return nil
	}

	engine := w.engine
	w.engine = nil
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
	w.failure = nil
	w.state = StateClosed
	w.mu.Unlock()

	return result
}
