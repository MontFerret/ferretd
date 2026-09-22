package workspace

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type (
	workspaceWatcher struct {
		mu sync.Mutex

		root      string
		backend   *fsnotify.Watcher
		watched   map[string]os.FileInfo
		processed chan watcherResult
		done      chan struct{}
		cancel    context.CancelFunc
		started   bool
		closed    bool
		closeErr  error
		closeOnce sync.Once
	}

	watcherResult struct {
		relativePath string
		err          error
	}

	workspaceWatcherFactory func(string) (*workspaceWatcher, error)
)

func newWorkspaceWatcher(root string) (*workspaceWatcher, error) {
	backend, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &workspaceWatcher{
		root:      root,
		backend:   backend,
		watched:   make(map[string]os.FileInfo),
		processed: make(chan watcherResult, 128),
		done:      make(chan struct{}),
	}, nil
}

func (w *workspaceWatcher) AddDirectory(relativePath string) error {
	_, err := w.addDirectory(relativePath)

	return err
}

// addDirectory reports whether this call installed a new watch. Callers serialize
// registration changes with the workspace mutation gate; the watcher lock also
// protects registration against concurrent close.
func (w *workspaceWatcher) addDirectory(relativePath string) (bool, error) {
	key := path.Clean(relativePath)
	absolute := filepath.Clean(filepath.Join(w.root, filepath.FromSlash(key)))

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return false, ErrClosed
	}

	info, err := watcherDirectoryInfo(absolute, key == ".")
	if err != nil {
		return false, err
	}

	if previous, ok := w.watched[absolute]; ok {
		if os.SameFile(previous, info) {
			return false, nil
		}

		if err := w.removeDirectoryLocked(absolute); err != nil {
			return false, err
		}
	}

	if err := w.backend.Add(absolute); err != nil {
		return false, err
	}

	current, err := watcherDirectoryInfo(absolute, key == ".")
	if err != nil || !os.SameFile(info, current) {
		removeErr := w.removeDirectoryLocked(absolute)

		if err == nil {
			err = errors.New("directory changed while adding watch")
		}

		return false, errors.Join(err, removeErr)
	}

	w.watched[absolute] = current

	return true, nil
}

func (w *workspaceWatcher) removeDirectories(directories []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var result error
	for _, directory := range directories {
		absolute := filepath.Clean(filepath.Join(w.root, filepath.FromSlash(directory)))
		result = errors.Join(result, w.removeDirectoryLocked(absolute))
	}

	return result
}

func (w *workspaceWatcher) removeDirectoryLocked(absolute string) error {
	err := w.backend.Remove(absolute)
	if errors.Is(err, fsnotify.ErrNonExistentWatch) || errors.Is(err, fsnotify.ErrClosed) ||
		isOnlyNotExist(err) || isInvalidatedWatchRemoval(err) {
		err = nil
	}

	delete(w.watched, absolute)

	return err
}

func (w *workspaceWatcher) Start(workspace *Workspace) {
	w.mu.Lock()
	if w.started || w.closed {
		w.mu.Unlock()

		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.started = true
	w.mu.Unlock()

	go w.run(ctx, workspace)
}

func (w *workspaceWatcher) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		cancel := w.cancel
		started := w.started
		w.mu.Unlock()

		if cancel != nil {
			cancel()
		}

		w.closeErr = w.backend.Close()

		if started {
			<-w.done
		} else {
			close(w.processed)
			close(w.done)
		}
	})

	return w.closeErr
}

func (w *workspaceWatcher) ReplaceSubtree(relativePath string, directories []string) error {
	keep := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		absolute := w.root

		if directory != "." {
			absolute = filepath.Join(w.root, filepath.FromSlash(directory))
		}

		keep[filepath.Clean(absolute)] = struct{}{}
	}

	prefix := w.root

	if relativePath != "." {
		prefix = filepath.Join(w.root, filepath.FromSlash(relativePath))
	}

	prefix = filepath.Clean(prefix)

	w.mu.Lock()
	defer w.mu.Unlock()

	var result error
	for watched := range w.watched {
		if !watcherPathAtOrBelow(watched, prefix) {
			continue
		}

		if _, ok := keep[watched]; ok {
			continue
		}

		result = errors.Join(result, w.removeDirectoryLocked(watched))
	}

	return result
}

func (w *workspaceWatcher) WatchesSubtree(relativePath string) bool {
	prefix := w.root

	if relativePath != "." {
		prefix = filepath.Join(w.root, filepath.FromSlash(relativePath))
	}

	prefix = filepath.Clean(prefix)

	w.mu.Lock()
	defer w.mu.Unlock()

	for watched := range w.watched {
		if watcherPathAtOrBelow(watched, prefix) {
			return true
		}
	}

	return false
}

func (w *workspaceWatcher) run(ctx context.Context, workspace *Workspace) {
	defer close(w.processed)
	defer close(w.done)

	events := w.backend.Events
	errorsChannel := w.backend.Errors
	for events != nil || errorsChannel != nil {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil

				continue
			}

			relativePath, accepted := w.relativePath(event.Name)
			if !accepted {
				continue
			}

			err := workspace.reconcileWatchEvent(ctx, event, relativePath)
			w.report(watcherResult{relativePath: relativePath, err: err})
		case _, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil

				continue
			}

			err := workspace.reconcileTree(ctx, ".")
			w.report(watcherResult{relativePath: ".", err: err})
		}
	}
}

func (w *workspaceWatcher) relativePath(absolutePath string) (string, bool) {
	relative, err := filepath.Rel(w.root, filepath.Clean(absolutePath))
	if err != nil || filepath.IsAbs(relative) || relative == ".." {
		return "", false
	}

	if prefix := ".." + string(filepath.Separator); len(relative) >= len(prefix) && relative[:len(prefix)] == prefix {
		return "", false
	}

	return path.Clean(filepath.ToSlash(relative)), true
}

func (w *workspaceWatcher) report(result watcherResult) {
	select {
	case w.processed <- result:
	default:
	}
}
