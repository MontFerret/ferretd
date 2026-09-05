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
		watched   map[string]struct{}
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
		watched:   make(map[string]struct{}),
		processed: make(chan watcherResult, 128),
		done:      make(chan struct{}),
	}, nil
}

func (w *workspaceWatcher) AddDirectory(relativePath string) error {
	key := path.Clean(relativePath)
	absolute := w.root

	if key != "." {
		absolute = filepath.Join(w.root, filepath.FromSlash(key))
	}

	absolute = filepath.Clean(absolute)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrClosed
	}

	if _, ok := w.watched[absolute]; ok {
		return nil
	}

	info, err := watcherDirectoryInfo(absolute, key == ".")
	if err != nil {
		return err
	}

	if err := w.backend.Add(absolute); err != nil {
		return err
	}

	current, err := watcherDirectoryInfo(absolute, key == ".")
	if err != nil || !os.SameFile(info, current) {
		removeErr := w.backend.Remove(absolute)
		if errors.Is(removeErr, fsnotify.ErrNonExistentWatch) || isOnlyNotExist(removeErr) {
			removeErr = nil
		}

		if err == nil {
			err = errors.New("directory changed while adding watch")
		}

		return errors.Join(err, removeErr)
	}

	w.watched[absolute] = struct{}{}

	return nil
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

		if err := w.backend.Remove(watched); err != nil &&
			!errors.Is(err, fsnotify.ErrNonExistentWatch) && !errors.Is(err, fsnotify.ErrClosed) &&
			!isOnlyNotExist(err) {
			result = errors.Join(result, err)
		}

		delete(w.watched, watched)
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
