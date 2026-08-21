package workspace

import (
	"context"
)

// RefreshDocument rereads one already-discovered document through its
// root-confined workspace filesystem. It retains the existing revision when
// the source state is unchanged and atomically publishes changed state.
func (w *Workspace) RefreshDocument(ctx context.Context, relativePath string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}

	if w.closing.Load() {
		return Document{}, ErrClosed
	}

	key, ok := normalizeDocumentPath(relativePath)
	if !ok {
		return Document{}, ErrDocumentNotFound
	}

	// Serialize refreshes so an older read can never overwrite a newer commit.
	// Filesystem I/O and parsing remain outside the main workspace state lock.
	select {
	case <-ctx.Done():
		return Document{}, ctx.Err()
	case <-w.refreshGate:
	}
	defer func() { w.refreshGate <- struct{}{} }()

	w.mu.RLock()
	if w.closing.Load() || w.state != StateReady {
		w.mu.RUnlock()

		return Document{}, ErrClosed
	}
	current, ok := w.documents[key]
	w.mu.RUnlock()

	if !ok {
		return Document{}, ErrDocumentNotFound
	}

	read, err := readDocument(ctx, w.root, current.File())
	if err != nil {
		return Document{}, err
	}

	loaded := read.err == nil
	if current.Loaded() == loaded && (!loaded || current.Content() == read.content) {
		w.mu.RLock()
		ready := !w.closing.Load() && w.state == StateReady
		w.mu.RUnlock()

		if !ready {
			return Document{}, ErrClosed
		}

		return current, nil
	}

	var refreshed Document
	if read.err != nil {
		refreshed = newUnreadableDocument(current.File(), read.err)
	} else {
		refreshed = newDocument(current.File(), read.content)
	}

	if err := ctx.Err(); err != nil {
		return Document{}, err
	}

	refreshed = refreshed.withRevision(current.Revision() + 1)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closing.Load() || w.state != StateReady {
		return Document{}, ErrClosed
	}

	if _, ok := w.documents[key]; !ok {
		return Document{}, ErrDocumentNotFound
	}

	w.documents[key] = refreshed

	return refreshed, nil
}
