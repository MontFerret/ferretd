package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
)

type documentRead struct {
	content string
	err     error
}

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

	key, ok := documentKey(relativePath)
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

func readDocument(ctx context.Context, rootPath string, file File) (documentRead, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return documentRead{err: fmt.Errorf("open workspace root: %w", err)}, nil
	}
	defer func() { _ = root.Close() }()

	pathInfo, err := root.Lstat(file.RelativePath)
	if err != nil {
		return documentRead{err: fmt.Errorf("inspect %q: %w", file.RelativePath, err)}, nil
	}
	if err := validateRefreshFile(file.RelativePath, pathInfo); err != nil {
		return documentRead{err: err}, nil
	}

	handle, err := root.Open(file.RelativePath)
	if err != nil {
		return documentRead{err: fmt.Errorf("open %q: %w", file.RelativePath, err)}, nil
	}
	defer func() { _ = handle.Close() }()

	openedInfo, err := handle.Stat()
	if err != nil {
		return documentRead{err: fmt.Errorf("inspect open %q: %w", file.RelativePath, err)}, nil
	}
	pathInfo, err = root.Lstat(file.RelativePath)
	if err != nil {
		return documentRead{err: fmt.Errorf("inspect %q: %w", file.RelativePath, err)}, nil
	}
	if err := validateRefreshFile(file.RelativePath, pathInfo); err != nil {
		return documentRead{err: err}, nil
	}
	if !openedInfo.Mode().IsRegular() {
		return documentRead{err: fmt.Errorf("inspect %q: source is not a regular file", file.RelativePath)}, nil
	}
	if !os.SameFile(openedInfo, pathInfo) {
		return documentRead{err: fmt.Errorf("inspect %q: source changed while opening", file.RelativePath)}, nil
	}

	bytes, err := io.ReadAll(handle)
	if err != nil {
		return documentRead{err: fmt.Errorf("read %q: %w", file.RelativePath, err)}, nil
	}
	if err := ctx.Err(); err != nil {
		return documentRead{}, err
	}

	return documentRead{content: string(bytes)}, nil
}

func validateRefreshFile(relativePath string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("inspect %q: symbolic links are not supported", relativePath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("inspect %q: source is not a regular file", relativePath)
	}

	return nil
}
