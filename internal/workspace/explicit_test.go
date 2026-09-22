package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestRefreshExplicitSourceAcrossDiscoveryBoundaries(t *testing.T) {
	for _, directory := range []string{".tmp", "_generated", "node_modules", "testdata", "vendor", "module", "module/child/module"} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			selected := directory + "/child/test.fql"
			neighbor := directory + "/child/neighbor.fql"
			writeWorkspaceSource(t, root, selected, "RETURN 1")
			writeWorkspaceSource(t, root, neighbor, "RETURN 2")
			writeWorkspaceSource(t, root, directory+"/other/ignored.fql", "RETURN 3")

			if directory == "module" || directory == "module/child/module" {
				writeWorkspaceSource(t, root, "module/go.mod", "module example.com/outer")
				writeWorkspaceSource(t, root, directory+"/go.mod", "module example.com/nested")
			}

			manager := newTestManager(t)

			opened, err := manager.Open(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}

			if len(opened.Documents()) != 0 {
				t.Fatalf("discovered excluded files: %v", opened.Files())
			}

			original, err := opened.RefreshDocument(context.Background(), selected)
			if err != nil {
				t.Fatalf("explicit RefreshDocument: %v", err)
			}

			if original.Content() != "RETURN 1" || original.File().Path != filepath.Join(opened.Root(), filepath.FromSlash(selected)) {
				t.Fatalf("selected source identity/content: %v %q", original.File(), original.Content())
			}

			for _, subtree := range []string{directory + "/child", directory, "."} {
				if err := opened.reconcileTree(context.Background(), subtree); err != nil {
					t.Fatal(err)
				}

				retained, found := opened.Document(selected)
				if !found || retained.Revision() != original.Revision() || retained.generation != original.generation || retained.syntax != original.syntax {
					t.Fatalf("reconcile %q changed selected source: %v", subtree, retained)
				}

				if got := len(opened.Documents()); got != 1 {
					t.Fatalf("reconcile %q admitted neighbors: %v", subtree, opened.Files())
				}
			}

			watcher := workspaceWatcherForTest(t, opened)
			if !watcher.WatchesSubtree(directory + "/child") {
				t.Fatal("selected parent is not watched")
			}

			if watcher.WatchesSubtree(directory + "/other") {
				t.Fatal("unselected excluded subtree is watched")
			}

			if err := opened.reconcileWatchEvent(context.Background(), fsnotify.Event{Op: fsnotify.Write}, neighbor); err != nil {
				t.Fatal(err)
			}

			if _, found := opened.Document(neighbor); found {
				t.Fatal("neighbor admitted by watcher")
			}
		})
	}
}

func TestExplicitSourceReconciliationLifecycle(t *testing.T) {
	root := t.TempDir()
	selected := ".tmp/nested/test.fql"
	writeWorkspaceSource(t, root, selected, "RETURN 1")
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	first, err := opened.RefreshDocument(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}

	writeWorkspaceSource(t, root, selected, "RETURN 2")

	if err := opened.reconcileWatchEvent(context.Background(), fsnotify.Event{Op: fsnotify.Write}, selected); err != nil {
		t.Fatal(err)
	}

	changed, _ := opened.Document(selected)
	if changed.Content() != "RETURN 2" || changed.Revision() != first.Revision()+1 || changed.generation <= first.generation {
		t.Fatalf("edit not retained: %v", changed)
	}

	if first.Content() != "RETURN 1" {
		t.Fatal("previous snapshot mutated")
	}

	if err := os.RemoveAll(filepath.Join(root, ".tmp", "nested")); err != nil {
		t.Fatal(err)
	}

	if err := opened.reconcileTree(context.Background(), ".tmp"); err != nil {
		t.Fatal(err)
	}

	if _, found := opened.Document(selected); found {
		t.Fatal("deleted source retained")
	}

	watcher := workspaceWatcherForTest(t, opened)
	if !watcher.WatchesSubtree(".tmp") || watcher.WatchesSubtree(".tmp/nested") {
		t.Fatal("missing source ancestor watch set is incorrect")
	}

	writeWorkspaceSource(t, root, selected, "RETURN 3")
	writeWorkspaceSource(t, root, ".tmp/nested/neighbor.fql", "RETURN 4")

	if err := opened.reconcileTree(context.Background(), ".tmp"); err != nil {
		t.Fatal(err)
	}

	restored, found := opened.Document(selected)
	if !found || restored.Content() != "RETURN 3" || restored.Revision() != 1 || restored.generation <= changed.generation {
		t.Fatalf("recreated source: %v", restored)
	}

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatal(err)
	}

	if len(opened.Documents()) != 1 {
		t.Fatal("root recovery admitted neighbor")
	}

	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatal(err)
	}

	if len(opened.explicitPaths) != 0 {
		t.Fatal("closed workspace retained explicit admissions")
	}

	if _, err := opened.RefreshDocument(context.Background(), selected); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed refresh: %v", err)
	}

	reopened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if len(reopened.Documents()) != 0 {
		t.Fatal("explicit admission survived workspace closure")
	}
}

func TestExplicitAdmissionSurvivesNewModuleBoundary(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "module/child/test.fql", "RETURN 1")
	writeWorkspaceSource(t, root, "module/child/neighbor.fql", "RETURN 2")
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	original, err := opened.RefreshDocument(context.Background(), "module/child/test.fql")
	if err != nil {
		t.Fatal(err)
	}

	writeWorkspaceSource(t, root, "module/go.mod", "module example.com/nested")

	if err := opened.reconcileWatchEvent(context.Background(), fsnotify.Event{Op: fsnotify.Create}, "module/go.mod"); err != nil {
		t.Fatal(err)
	}

	retained, found := opened.Document("module/child/test.fql")
	if !found || retained.generation != original.generation || retained.Revision() != original.Revision() {
		t.Fatal("new module boundary lost explicit source")
	}

	if len(opened.Documents()) != 1 {
		t.Fatal("module boundary retained unselected neighbor")
	}
}

func TestExplicitAdmissionRejectsInvalidSources(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, ".tmp/notes.txt", "RETURN 1")
	writeWorkspaceSource(t, root, ".tmp/upper.FQL", "RETURN 1")
	writeWorkspaceSource(t, root, ".tmp/real/test.fql", "RETURN 1")

	if err := os.Mkdir(filepath.Join(root, ".tmp", "directory.fql"), 0o700); err != nil {
		t.Fatal(err)
	}

	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	paths := []string{".tmp/missing.fql", ".tmp/notes.txt", ".tmp/upper.FQL", ".tmp/directory.fql", "../outside.fql", filepath.Join(root, ".tmp/real/test.fql")}
	for _, relative := range paths {
		if _, err := opened.RefreshDocument(context.Background(), relative); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("%q: %v", relative, err)
		}
	}

	t.Run("symlinks", func(t *testing.T) {
		if err := os.Symlink("real", filepath.Join(root, ".tmp", "linked")); err != nil {
			t.Skipf("symlink: %v", err)
		}

		if err := os.Symlink("real/test.fql", filepath.Join(root, ".tmp", "linked.fql")); err != nil {
			t.Fatal(err)
		}

		outside := t.TempDir()
		writeWorkspaceSource(t, outside, "outside.fql", "RETURN 9")

		if err := os.Symlink(outside, filepath.Join(root, ".tmp", "outside")); err != nil {
			t.Fatal(err)
		}

		for _, relative := range []string{".tmp/linked/test.fql", ".tmp/linked.fql", ".tmp/outside/outside.fql"} {
			if _, err := opened.RefreshDocument(context.Background(), relative); !errors.Is(err, ErrDocumentNotFound) {
				t.Fatalf("%q: %v", relative, err)
			}
		}
	})

	if len(opened.Documents()) != 0 {
		t.Fatalf("rejected sources retained: %v", opened.Files())
	}

	if workspaceWatcherForTest(t, opened).WatchesSubtree(".tmp") {
		t.Fatal("rejected selections leaked ancestor watches")
	}
}

func TestExplicitAdmissionCancellationDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	selected := ".tmp/test.fql"
	writeWorkspaceSource(t, root, selected, "RETURN 1")
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	<-opened.mutationGate
	ctx, cancel := context.WithCancel(context.Background())
	observed := newObservedDoneContext(ctx)
	result := make(chan error, 1)
	go func() { _, err := opened.RefreshDocument(observed, selected); result <- err }()
	<-observed.observed
	cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled refresh: %v", err)
	}

	opened.mutationGate <- struct{}{}

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatal(err)
	}

	if len(opened.Documents()) != 0 {
		t.Fatal("canceled selection admitted")
	}
}

func TestExplicitSourceWatcherAndOverflow(t *testing.T) {
	root := t.TempDir()
	selected := ".tmp/nested/test.fql"
	writeWorkspaceSource(t, root, selected, "RETURN 1")

	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	watcher := workspaceWatcherForTest(t, opened)
	// The backend owns and closes its real error channel. Inject errors through
	// a test-owned channel before starting the consumer to avoid send/close races.
	errorsChannel := make(chan error)
	watcher.backend.Errors = errorsChannel
	watcher.Start(opened)

	if _, err := opened.RefreshDocument(context.Background(), selected); err != nil {
		t.Fatal(err)
	}

	writeWorkspaceSource(t, root, selected, "RETURN 2")
	waitForWorkspaceChange(t, opened, "explicit source edit", func() bool { doc, ok := opened.Document(selected); return ok && doc.Content() == "RETURN 2" })

	if err := os.Remove(filepath.Join(root, filepath.FromSlash(selected))); err != nil {
		t.Fatal(err)
	}

	waitForWorkspaceChange(t, opened, "explicit source removal", func() bool { _, ok := opened.Document(selected); return !ok })
	writeWorkspaceSource(t, root, selected, "RETURN 3")
	waitForWorkspaceChange(t, opened, "explicit source recreation", func() bool { doc, ok := opened.Document(selected); return ok && doc.Content() == "RETURN 3" })

	if err := os.RemoveAll(filepath.Join(root, ".tmp")); err != nil {
		t.Fatal(err)
	}

	waitForWorkspaceChange(t, opened, "selected ancestor removal", func() bool {
		_, ok := opened.Document(selected)

		return !ok && !watcher.WatchesSubtree(".tmp")
	})
	writeWorkspaceSource(t, root, selected, "RETURN 4")
	waitForWorkspaceChange(t, opened, "selected ancestor recreation", func() bool {
		doc, ok := opened.Document(selected)

		return ok && doc.Content() == "RETURN 4"
	})
	writeWorkspaceSource(t, root, ".tmp/nested/neighbor.fql", "RETURN 4")
	waitForWatcherPath(t, watcher, ".tmp/nested/neighbor.fql")
	errorsChannel <- fsnotify.ErrEventOverflow
	waitForWatcherPath(t, watcher, ".")

	if got := opened.Files(); len(got) != 1 || got[0].RelativePath != selected {
		t.Fatalf("overflow lost explicit source or admitted neighbors: %v", got)
	}

	watcher.mu.Lock()
	watched := make(map[string]struct{}, len(watcher.watched))
	for key := range watcher.watched {
		watched[key] = struct{}{}
	}

	watcher.mu.Unlock()

	want := map[string]struct{}{opened.Root(): {}, filepath.Join(opened.Root(), ".tmp"): {}, filepath.Join(opened.Root(), ".tmp", "nested"): {}}
	if !reflect.DeepEqual(watched, want) {
		t.Fatalf("watched paths = %v, want %v", watched, want)
	}
}

func TestExplicitAdmissionDoesNotFollowReplacedAncestor(t *testing.T) {
	root := t.TempDir()
	selected := ".tmp/nested/test.fql"
	writeWorkspaceSource(t, root, selected, "RETURN 1")
	outside := t.TempDir()
	writeWorkspaceSource(t, outside, "test.fql", "RETURN 99")
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := opened.RefreshDocument(context.Background(), selected); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(root, ".tmp", "nested")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(outside, filepath.Join(root, ".tmp", "nested")); err != nil {
		t.Skipf("symlink: %v", err)
	}

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatal(err)
	}

	if _, found := opened.Document(selected); found {
		t.Fatal("selected path followed replaced ancestor")
	}

	if workspaceWatcherForTest(t, opened).WatchesSubtree(".tmp/nested") {
		t.Fatal("symlink replacement remains watched")
	}

	if _, err := opened.RefreshDocument(context.Background(), selected); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("symlink refresh: %v", err)
	}
}

func TestMissingExplicitSelectionDoesNotAdmitLaterCreation(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := opened.RefreshDocument(context.Background(), ".tmp/test.fql"); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatal(err)
	}

	writeWorkspaceSource(t, root, ".tmp/test.fql", "RETURN 1")

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatal(err)
	}

	if len(opened.Documents()) != 0 {
		t.Fatal("unsuccessful selection retained admission")
	}
}

func TestCanceledExplicitAdmissionRollsBackNewWatches(t *testing.T) {
	root := t.TempDir()
	selected := ".tmp/nested/test.fql"
	writeWorkspaceSource(t, root, selected, "RETURN 1")
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	watcher := workspaceWatcherForTest(t, opened)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	canceled := &watchCancellationContext{Context: ctx, cancel: cancel, watcher: watcher, directory: ".tmp/nested"}
	if _, err := opened.RefreshDocument(canceled, selected); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled refresh: %v", err)
	}

	if watcher.WatchesSubtree(".tmp") {
		t.Fatal("canceled admission leaked ancestor watches")
	}

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatal(err)
	}

	if len(opened.Documents()) != 0 {
		t.Fatal("canceled admission was retained")
	}

	if _, err := opened.RefreshDocument(context.Background(), selected); err != nil {
		t.Fatalf("retry after canceled admission: %v", err)
	}
}

func TestExplicitSourceOverflowReplacesOldDirectoryWatch(t *testing.T) {
	root := t.TempDir()
	selected := ".tmp/nested/test.fql"
	writeWorkspaceSource(t, root, selected, "RETURN 1")
	manager := newTestManager(t)

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	original, err := opened.RefreshDocument(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}

	// The watcher loop is deliberately stopped. Recovery cannot depend on seeing
	// the rename before the replacement appears at the same path.
	if err := os.Rename(filepath.Join(root, ".tmp"), filepath.Join(root, ".old")); err != nil {
		t.Fatal(err)
	}

	writeWorkspaceSource(t, root, selected, "RETURN 2")

	if err := opened.reconcileTree(context.Background(), "."); err != nil {
		t.Fatal(err)
	}

	restored, found := opened.Document(selected)
	if !found || restored.Content() != "RETURN 2" || restored.Revision() != original.Revision()+1 || restored.generation <= original.generation {
		t.Fatalf("replacement source = %v", restored)
	}

	watcher := workspaceWatcherForTest(t, opened)

	actual, err := os.Stat(filepath.Join(opened.Root(), ".tmp", "nested"))
	if err != nil {
		t.Fatal(err)
	}

	watcher.mu.Lock()
	retained := watcher.watched[filepath.Join(opened.Root(), ".tmp", "nested")]
	watcher.mu.Unlock()

	if retained == nil || !os.SameFile(retained, actual) {
		t.Fatal("overflow retained old directory watch identity")
	}

	if len(opened.Documents()) != 1 {
		t.Fatal("overflow admitted renamed excluded neighbor")
	}
}
