package workspace

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/fsnotify/fsnotify"
)

// RefreshDocument reconciles one workspace-relative source with the filesystem.
// Explicit selection bypasses directory discovery exclusions, but still requires
// a regular lowercase .fql file beneath the workspace without nested symlinks.
// Successful admissions persist until close, including across deletion/recreation.
func (w *Workspace) RefreshDocument(ctx context.Context, relativePath string) (Document, error) {
	document, found, err := w.reconcileDocument(ctx, relativePath, true)
	if err != nil {
		return Document{}, err
	}

	if !found {
		return Document{}, ErrDocumentNotFound
	}

	return document, nil
}

func (w *Workspace) reconcileDocument(
	ctx context.Context,
	relativePath string,
	explicit bool,
) (document Document, found bool, err error) {
	if err := w.beginMutation(ctx); err != nil {
		return Document{}, false, err
	}

	defer w.finishMutation()

	key, ok := normalizeDocumentPath(relativePath)
	if !ok {
		return Document{}, false, nil
	}

	w.mu.RLock()
	watcher := w.watcher
	_, admitted := w.explicitPaths[key]
	w.mu.RUnlock()

	registration := watchRegistration{watcher: watcher}
	defer registration.finish(&err)

	discovered, err := discoverWorkspaceDocument(ctx, w.root, key, explicit || admitted, registration.observe)
	if err != nil {
		return Document{}, false, err
	}

	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closing.Load() || w.state != StateReady {
		return Document{}, false, ErrClosed
	}

	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}

	current, exists := w.documents[key]

	if !discovered.found {
		registration.committed = !explicit || admitted

		if exists {
			delete(w.documents, key)
			w.rebuildIndexesLocked()
		}

		return Document{}, false, nil
	}

	registration.committed = true

	if explicit {
		if w.explicitPaths == nil {
			w.explicitPaths = make(map[string]struct{})
		}

		w.explicitPaths[key] = struct{}{}
	}

	next := discovered.document
	if exists && current.sameState(next) {
		return current, true, nil
	}

	if exists {
		next = next.withRevision(current.Revision() + 1)
	}

	w.nextDocumentGeneration++
	next = next.withGeneration(w.nextDocumentGeneration)
	w.documents[key] = next

	if !exists {
		w.rebuildIndexesLocked()
	}

	return next, true, nil
}

func (w *Workspace) reconcileTree(ctx context.Context, relativePath string) (err error) {
	if err := w.beginMutation(ctx); err != nil {
		return err
	}

	defer w.finishMutation()

	key, ok := normalizeWorkspacePath(relativePath)
	if !ok {
		return nil
	}

	w.mu.RLock()
	watcher := w.watcher
	var explicitPaths []string
	for selected := range w.explicitPaths {
		if workspacePathInSubtree(selected, key) {
			explicitPaths = append(explicitPaths, selected)
		}
	}

	w.mu.RUnlock()
	sort.Strings(explicitPaths)

	registration := watchRegistration{watcher: watcher}
	defer registration.finish(&err)

	content, err := loadWorkspaceSubtree(ctx, w.root, key, registration.observe)
	if err != nil {
		return err
	}

	if err := w.reconcileExplicitPaths(ctx, &content, explicitPaths, registration.observe); err != nil {
		return err
	}

	w.mu.Lock()
	if err := ctx.Err(); err != nil {
		w.mu.Unlock()

		return err
	}

	if w.closing.Load() || w.state != StateReady {
		w.mu.Unlock()

		return ErrClosed
	}

	w.applyContentLocked(key, content)
	registration.committed = true
	w.mu.Unlock()

	if watcher != nil {
		if err := watcher.ReplaceSubtree(key, content.directories); err != nil && !errors.Is(err, ErrClosed) {
			return err
		}
	}

	return nil
}

// Reconciliation reads selected files individually; it never walks their excluded
// neighbors. Existing ancestors remain watched even while a selected file is absent.
func (w *Workspace) reconcileExplicitPaths(
	ctx context.Context,
	content *workspaceContent,
	explicitPaths []string,
	observe directoryObserver,
) error {
	for _, key := range explicitPaths {
		if _, found := content.documents[key]; found {
			continue
		}

		discovered, err := discoverWorkspaceDocument(ctx, w.root, key, true, observe)
		if err != nil {
			return err
		}

		content.directories = append(content.directories, discovered.directories...)

		if discovered.found {
			content.documents[key] = discovered.document
			content.order = append(content.order, key)
		}
	}

	sort.Strings(content.order)

	return ctx.Err()
}

func (w *Workspace) reconcileWatchEvent(
	ctx context.Context,
	event fsnotify.Event,
	relativePath string,
) error {
	key, ok := normalizeWorkspacePath(relativePath)
	if !ok {
		return nil
	}

	if key == "." {
		return w.reconcileTree(ctx, key)
	}

	base := path.Base(key)
	if base == "go.mod" {
		return w.reconcileTree(ctx, path.Dir(key))
	}

	if event.Has(fsnotify.Rename) {
		return w.reconcileTree(ctx, path.Dir(key))
	}

	if isWorkspaceSource(base) {
		_, _, err := w.reconcileDocument(ctx, key, false)

		return err
	}

	absolute := filepath.Join(w.root, filepath.FromSlash(key))

	info, err := os.Lstat(absolute)
	if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return w.reconcileTree(ctx, key)
	}

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	w.mu.RLock()
	watcher := w.watcher
	w.mu.RUnlock()

	if watcher != nil && watcher.WatchesSubtree(key) {
		return w.reconcileTree(ctx, key)
	}

	return nil
}

func (w *Workspace) beginMutation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if w.closing.Load() {
		return ErrClosed
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.mutationGate:
	}

	if w.closing.Load() {
		w.finishMutation()

		return ErrClosed
	}

	return nil
}

func (w *Workspace) finishMutation() {
	w.mutationGate <- struct{}{}
}

func (w *Workspace) applyContentLocked(relativePath string, content workspaceContent) {
	changedMembership := false
	for key := range w.documents {
		if !workspacePathInSubtree(key, relativePath) {
			continue
		}

		if _, ok := content.documents[key]; ok {
			continue
		}

		delete(w.documents, key)
		changedMembership = true
	}

	for _, key := range content.order {
		next := content.documents[key]

		current, exists := w.documents[key]
		if exists && current.sameState(next) {
			continue
		}

		if exists {
			next = next.withRevision(current.Revision() + 1)
		} else {
			changedMembership = true
		}

		w.nextDocumentGeneration++
		next = next.withGeneration(w.nextDocumentGeneration)
		w.documents[key] = next
	}

	if changedMembership {
		w.rebuildIndexesLocked()
	}
}

func (w *Workspace) rebuildIndexesLocked() {
	w.order = w.order[:0]

	for relativePath := range w.documents {
		w.order = append(w.order, relativePath)
	}

	sort.Strings(w.order)

	w.files = make([]File, 0, len(w.order))
	for _, relativePath := range w.order {
		w.files = append(w.files, w.documents[relativePath].File())
	}
}
