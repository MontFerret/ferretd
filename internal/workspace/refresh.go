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
// A currently eligible source is admitted even when it was not present during
// initial discovery; a missing or ineligible source is removed from retained state.
func (w *Workspace) RefreshDocument(ctx context.Context, relativePath string) (Document, error) {
	document, found, err := w.reconcileDocument(ctx, relativePath)
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
) (Document, bool, error) {
	if err := w.beginMutation(ctx); err != nil {
		return Document{}, false, err
	}
	defer w.finishMutation()

	key, ok := normalizeDocumentPath(relativePath)
	if !ok {
		return Document{}, false, nil
	}

	discovered, err := discoverWorkspaceDocument(ctx, w.root, key)
	if err != nil {
		return Document{}, false, err
	}

	w.mu.RLock()
	watcher := w.watcher
	w.mu.RUnlock()
	if watcher != nil {
		for _, directory := range discovered.directories {
			if err := watcher.AddDirectory(directory); err != nil {
				if directory != "." && isOnlyNotExist(err) {
					discovered.found = false

					break
				}

				return Document{}, false, err
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closing.Load() || w.state != StateReady {
		return Document{}, false, ErrClosed
	}

	current, exists := w.documents[key]
	if !discovered.found {
		if exists {
			delete(w.documents, key)
			w.rebuildIndexesLocked()
		}

		return Document{}, false, nil
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

func (w *Workspace) reconcileTree(ctx context.Context, relativePath string) error {
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
	w.mu.RUnlock()

	var observe directoryObserver
	if watcher != nil {
		observe = watcher.AddDirectory
	}

	content, err := loadWorkspaceSubtree(ctx, w.root, key, observe)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	w.mu.Lock()
	if w.closing.Load() || w.state != StateReady {
		w.mu.Unlock()

		return ErrClosed
	}

	w.applyContentLocked(key, content)
	w.mu.Unlock()

	if watcher != nil {
		if err := watcher.ReplaceSubtree(key, content.directories); err != nil && !errors.Is(err, ErrClosed) {
			return err
		}
	}

	return nil
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
		_, _, err := w.reconcileDocument(ctx, key)

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
