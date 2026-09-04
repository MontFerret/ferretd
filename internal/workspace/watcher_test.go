package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceWatcherReconcilesFilesystemChanges(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "existing.fql", "RETURN 1")
	manager, opened := openWatchedWorkspace(t, root)

	writeWorkspaceSource(t, root, "created.fql", "RETURN 2")
	waitForWorkspaceChange(t, opened, "created source", func() bool {
		document, ok := opened.Document("created.fql")

		return ok && document.Content() == "RETURN 2"
	})

	created, _ := opened.Document("created.fql")
	writeWorkspaceSource(t, root, "created.fql", "RETURN 3")
	waitForWorkspaceChange(t, opened, "updated source", func() bool {
		document, ok := opened.Document("created.fql")

		return ok && document.Content() == "RETURN 3" && document.Revision() > created.Revision()
	})

	if err := os.Rename(filepath.Join(root, "created.fql"), filepath.Join(root, "renamed.fql")); err != nil {
		t.Fatalf("Rename .fql to .fql: %v", err)
	}
	waitForWorkspaceChange(t, opened, "renamed source", func() bool {
		_, oldFound := opened.Document("created.fql")
		renamed, newFound := opened.Document("renamed.fql")

		return !oldFound && newFound && renamed.Content() == "RETURN 3"
	})

	if err := os.Rename(filepath.Join(root, "renamed.fql"), filepath.Join(root, "notes.txt")); err != nil {
		t.Fatalf("Rename .fql to non-.fql: %v", err)
	}
	waitForWorkspaceChange(t, opened, "source renamed to non-source", func() bool {
		_, found := opened.Document("renamed.fql")

		return !found
	})

	if err := os.Rename(filepath.Join(root, "notes.txt"), filepath.Join(root, "restored.fql")); err != nil {
		t.Fatalf("Rename non-.fql to .fql: %v", err)
	}
	waitForWorkspaceChange(t, opened, "non-source renamed to source", func() bool {
		document, found := opened.Document("restored.fql")

		return found && document.Content() == "RETURN 3"
	})

	writeWorkspaceSource(t, root, "new-directory/nested.fql", "RETURN 4")
	waitForWorkspaceChange(t, opened, "new directory source", func() bool {
		document, found := opened.Document("new-directory/nested.fql")

		return found && document.Content() == "RETURN 4"
	})

	if err := os.Rename(filepath.Join(root, "new-directory"), filepath.Join(root, "moved-directory")); err != nil {
		t.Fatalf("Rename directory: %v", err)
	}
	waitForWorkspaceChange(t, opened, "renamed directory", func() bool {
		_, oldFound := opened.Document("new-directory/nested.fql")
		moved, newFound := opened.Document("moved-directory/nested.fql")

		return !oldFound && newFound && moved.Content() == "RETURN 4"
	})

	if err := os.RemoveAll(filepath.Join(root, "moved-directory")); err != nil {
		t.Fatalf("Remove directory: %v", err)
	}
	waitForWorkspaceChange(t, opened, "deleted directory", func() bool {
		_, found := opened.Document("moved-directory/nested.fql")

		return !found
	})

	watcher := workspaceWatcherForTest(t, opened)
	for _, directory := range []string{".hidden", "_generated", "node_modules", "testdata", "vendor"} {
		writeWorkspaceSource(t, root, directory+"/ignored.fql", "RETURN 5")
		waitForWatcherPath(t, watcher, directory)
		if _, found := opened.Document(directory + "/ignored.fql"); found {
			t.Fatalf("dynamically created excluded source %q was retained", directory)
		}
	}

	outside := t.TempDir()
	writeWorkspaceSource(t, outside, "outside.fql", "RETURN 6")
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}
	waitForWatcherPath(t, watcher, "linked-directory")
	if _, found := opened.Document("linked-directory/outside.fql"); found {
		t.Fatal("dynamic directory symlink source was retained")
	}

	writeWorkspaceSource(t, root, "invalid.fql", "RETURN")
	waitForWorkspaceChange(t, opened, "invalid source diagnostics", func() bool {
		document, found := opened.Document("invalid.fql")

		return found && len(document.Diagnostics()) != 0
	})
	if err := os.Remove(filepath.Join(root, "invalid.fql")); err != nil {
		t.Fatalf("Remove invalid source: %v", err)
	}
	waitForWorkspaceChange(t, opened, "removed invalid source", func() bool {
		_, found := opened.Document("invalid.fql")

		return !found
	})
	for _, file := range opened.Files() {
		if file.RelativePath == "invalid.fql" {
			t.Fatal("removed source remains in workspace file index")
		}
	}
	for _, diagnostic := range opened.Diagnostics() {
		if !diagnostic.Source.Empty() && diagnostic.Source.Name() == filepath.Join(root, "invalid.fql") {
			t.Fatal("removed source diagnostics remain retained")
		}
	}

	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-watcher.done:
	default:
		t.Fatal("workspace close returned before watcher stopped")
	}
}

func TestWorkspaceWatcherTracksDynamicNestedModuleBoundaries(t *testing.T) {
	root := t.TempDir()
	manager, opened := openWatchedWorkspace(t, root)

	writeWorkspaceSource(t, root, "module/query.fql", "RETURN 1")
	waitForWorkspaceChange(t, opened, "initial nested source", func() bool {
		_, found := opened.Document("module/query.fql")

		return found
	})

	writeWorkspaceSource(t, root, "module/go.mod", "module example.com/nested")
	waitForWorkspaceChange(t, opened, "nested module boundary", func() bool {
		_, found := opened.Document("module/query.fql")

		return !found
	})

	watcher := workspaceWatcherForTest(t, opened)
	if !watcher.WatchesSubtree("module") {
		t.Fatal("nested module boundary directory is not watched")
	}
	if watcher.WatchesSubtree("module/child") {
		t.Fatal("nested module descendants remain watched")
	}

	if err := os.Remove(filepath.Join(root, "module", "go.mod")); err != nil {
		t.Fatalf("Remove nested go.mod: %v", err)
	}
	waitForWorkspaceChange(t, opened, "removed nested module boundary", func() bool {
		document, found := opened.Document("module/query.fql")

		return found && document.Content() == "RETURN 1"
	})

	writeWorkspaceSource(t, root, "module/boundary.txt", "module example.com/nested")
	if err := os.Rename(filepath.Join(root, "module", "boundary.txt"), filepath.Join(root, "module", "go.mod")); err != nil {
		t.Fatalf("Rename nested go.mod into place: %v", err)
	}
	waitForWorkspaceChange(t, opened, "renamed nested module boundary", func() bool {
		_, found := opened.Document("module/query.fql")

		return !found
	})

	if err := os.Rename(filepath.Join(root, "module", "go.mod"), filepath.Join(root, "module", "go.mod.saved")); err != nil {
		t.Fatalf("Rename nested go.mod away: %v", err)
	}
	waitForWorkspaceChange(t, opened, "nested module boundary renamed away", func() bool {
		document, found := opened.Document("module/query.fql")

		return found && document.Content() == "RETURN 1"
	})

	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWorkspaceWatcherIgnoresLateEventsAfterClose(t *testing.T) {
	root := t.TempDir()
	manager, opened := openWatchedWorkspace(t, root)
	watcher := workspaceWatcherForTest(t, opened)

	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	writeWorkspaceSource(t, root, "late.fql", "RETURN 1")

	select {
	case <-watcher.done:
	default:
		t.Fatal("watcher remains active after close")
	}
	if opened.State() != StateClosed || len(opened.Documents()) != 0 {
		t.Fatalf("closed workspace state = %v documents = %d", opened.State(), len(opened.Documents()))
	}
}

func TestWorkspaceWatcherClearStopsAllWatchers(t *testing.T) {
	manager := New()
	var opened []*Workspace
	var watchers []*workspaceWatcher
	for range 2 {
		workspace, err := manager.Open(context.Background(), t.TempDir())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		opened = append(opened, workspace)
		watchers = append(watchers, workspaceWatcherForTest(t, workspace))
	}

	if err := manager.Clear(context.Background()); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	for index, watcher := range watchers {
		select {
		case <-watcher.done:
		default:
			t.Fatalf("watcher %d remains active after Clear", index)
		}
		if opened[index].State() != StateClosed {
			t.Fatalf("workspace %d state = %v, want StateClosed", index, opened[index].State())
		}
	}
}

func TestWorkspaceWatcherOpenFailureDoesNotPublishWorkspace(t *testing.T) {
	manager := New()
	want := errors.New("watcher unavailable")
	manager.newWatcher = func(string) (*workspaceWatcher, error) {
		return nil, want
	}

	_, err := manager.Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrLoad) || !errors.Is(err, want) {
		t.Fatalf("Open error = %v, want ErrLoad and watcher cause", err)
	}
	items, listErr := manager.List(context.Background())
	if listErr != nil || len(items) != 0 {
		t.Fatalf("List after failed watcher setup = %#v, %v", items, listErr)
	}
}

func TestWorkspaceRootReconciliationRecoversMissedAndDuplicateEvents(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "removed.fql", "RETURN 1")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "removed.fql")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	writeWorkspaceSource(t, root, "created/query.fql", "RETURN 2")
	writeWorkspaceSource(t, root, "created/vendor/ignored.fql", "RETURN 3")

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatalf("reconcile root: %v", err)
	}
	if _, found := opened.Document("removed.fql"); found {
		t.Fatal("root reconciliation retained removed source")
	}
	created, found := opened.Document("created/query.fql")
	if !found || created.Content() != "RETURN 2" {
		t.Fatalf("root reconciliation created source = %#v, %t", created, found)
	}
	if _, found := opened.Document("created/vendor/ignored.fql"); found {
		t.Fatal("root reconciliation retained excluded source")
	}

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatalf("duplicate reconcile root: %v", err)
	}
	unchanged, found := opened.Document("created/query.fql")
	if !found || unchanged.Revision() != created.Revision() ||
		unchanged.generation != created.generation || unchanged.syntax != created.syntax {
		t.Fatalf("duplicate reconciliation changed document: before=%#v after=%#v", created, unchanged)
	}
}

func openWatchedWorkspace(t *testing.T, root string) (*Manager, *Workspace) {
	t.Helper()

	manager := New()
	t.Cleanup(func() {
		if err := manager.Clear(context.Background()); err != nil {
			t.Errorf("Clear: %v", err)
		}
	})

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return manager, opened
}

func workspaceWatcherForTest(t *testing.T, workspace *Workspace) *workspaceWatcher {
	t.Helper()

	workspace.mu.RLock()
	watcher := workspace.watcher
	workspace.mu.RUnlock()
	if watcher == nil {
		t.Fatal("workspace has no watcher")
	}

	return watcher
}

func waitForWorkspaceChange(
	t *testing.T,
	workspace *Workspace,
	description string,
	condition func() bool,
) {
	t.Helper()

	if condition() {
		return
	}

	watcher := workspaceWatcherForTest(t, workspace)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case result, ok := <-watcher.processed:
			if !ok {
				t.Fatalf("watcher stopped before observing %s", description)
			}
			if result.err != nil {
				t.Fatalf("watcher reconcile %q: %v", result.relativePath, result.err)
			}
			if condition() {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func waitForWatcherPath(t *testing.T, watcher *workspaceWatcher, prefix string) {
	t.Helper()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case result, ok := <-watcher.processed:
			if !ok {
				t.Fatalf("watcher stopped before observing %q", prefix)
			}
			if result.err != nil {
				t.Fatalf("watcher reconcile %q: %v", result.relativePath, result.err)
			}
			if result.relativePath == prefix || strings.HasPrefix(result.relativePath, prefix+"/") {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for watcher path %q", prefix)
		}
	}
}
