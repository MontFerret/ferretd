package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWorkspaceWatcherRemovesInvalidatedWatch(t *testing.T) {
	root := t.TempDir()

	directory := filepath.Join(root, "removed")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	watcher, err := newWorkspaceWatcher(root)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	t.Cleanup(func() {
		if err := watcher.Close(); err != nil {
			t.Errorf("close watcher: %v", err)
		}
	})

	for _, relativePath := range []string{".", "removed"} {
		if err := watcher.AddDirectory(relativePath); err != nil {
			t.Fatalf("watch %q: %v", relativePath, err)
		}
	}

	// Leave the unbuffered backend events unread. The child's create event
	// blocks delivery before fsnotify can process the directory's deletion,
	// while the kernel invalidates its watch as soon as the directory is removed.
	child := filepath.Join(directory, "child.fql")
	if err := os.WriteFile(child, []byte("RETURN 1"), 0o600); err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := os.Remove(child); err != nil {
		t.Fatalf("remove child: %v", err)
	}

	if err := os.Remove(directory); err != nil {
		t.Fatalf("remove directory: %v", err)
	}

	if !slices.Contains(watcher.backend.WatchList(), directory) {
		t.Fatal("backend processed the directory deletion before watch removal")
	}

	for attempt := range 2 {
		if err := watcher.ReplaceSubtree(".", []string{"."}); err != nil {
			t.Fatalf("replace subtree attempt %d: %v", attempt+1, err)
		}

		if watcher.WatchesSubtree("removed") {
			t.Fatal("removed directory remains in watcher bookkeeping")
		}

		if watched := watcher.backend.WatchList(); !slices.Equal(watched, []string{root}) {
			t.Fatalf("backend watches = %v, want only root %q", watched, root)
		}
	}
}
