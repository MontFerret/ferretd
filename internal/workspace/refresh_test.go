package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestRefreshDocumentRetainsAndAdvancesSourceRevisions(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	original, ok := opened.Document("query.fql")
	if !ok {
		t.Fatal("Document query.fql not found")
	}
	unchanged, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("unchanged RefreshDocument: %v", err)
	}
	if unchanged.Revision() != 1 || unchanged.Content() != "RETURN 1" || unchanged.syntax != original.syntax {
		t.Fatalf("unchanged document = revision %d content %q syntax %p, want retained revision and syntax %p",
			unchanged.Revision(), unchanged.Content(), unchanged.syntax, original.syntax)
	}

	writeWorkspaceSource(t, root, "query.fql", "RETURN 2")
	changed, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("changed RefreshDocument: %v", err)
	}
	if changed.Revision() != 2 || changed.Content() != "RETURN 2" || !changed.Loaded() {
		t.Fatalf("changed document = revision %d loaded %t content %q",
			changed.Revision(), changed.Loaded(), changed.Content())
	}

	writeWorkspaceSource(t, root, "query.fql", "RETURN")
	invalid, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("invalid RefreshDocument: %v", err)
	}
	if invalid.Revision() != 3 || !invalid.Loaded() || len(invalid.Diagnostics()) == 0 {
		t.Fatalf("invalid document = revision %d loaded %t diagnostics %d",
			invalid.Revision(), invalid.Loaded(), len(invalid.Diagnostics()))
	}

	if err := os.Remove(filepath.Join(root, "query.fql")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := opened.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("missing RefreshDocument error = %v, want ErrDocumentNotFound", err)
	}
	if _, ok := opened.Document("query.fql"); ok {
		t.Fatal("missing document remains retained")
	}
	if _, err := opened.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("repeated missing RefreshDocument error = %v, want ErrDocumentNotFound", err)
	}

	writeWorkspaceSource(t, root, "query.fql", "RETURN 3")
	restored, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("restored RefreshDocument: %v", err)
	}
	if restored.Revision() != 1 || restored.generation <= invalid.generation ||
		!restored.Loaded() || restored.Content() != "RETURN 3" {
		t.Fatalf("restored document = revision %d loaded %t content %q",
			restored.Revision(), restored.Loaded(), restored.Content())
	}
}

func TestRefreshDocumentRejectsInvalidReplacementsAndUndiscoveredPaths(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
		manager := newTestManager(t)
		opened, err := manager.Open(context.Background(), root)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		writeWorkspaceSource(t, root, "target.txt", "RETURN 2")
		if err := os.Remove(filepath.Join(root, "query.fql")); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if err := os.Symlink("target.txt", filepath.Join(root, "query.fql")); err != nil {
			t.Skipf("create symlink: %v", err)
		}

		if _, err := opened.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("RefreshDocument error = %v, want ErrDocumentNotFound", err)
		}
		if _, ok := opened.Document("query.fql"); ok {
			t.Fatal("symlink replacement remains retained")
		}
	})

	t.Run("non_regular", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
		manager := newTestManager(t)
		opened, err := manager.Open(context.Background(), root)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		if err := os.Remove(filepath.Join(root, "query.fql")); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if err := os.Mkdir(filepath.Join(root, "query.fql"), 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}

		if _, err := opened.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("RefreshDocument error = %v, want ErrDocumentNotFound", err)
		}
		if _, ok := opened.Document("query.fql"); ok {
			t.Fatal("non-regular replacement remains retained")
		}
	})

	t.Run("undiscovered", func(t *testing.T) {
		root := t.TempDir()
		writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
		manager := newTestManager(t)
		opened, err := manager.Open(context.Background(), root)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		writeWorkspaceSource(t, root, "new.fql", "RETURN 2")

		document, err := opened.RefreshDocument(context.Background(), "new.fql")
		if err != nil {
			t.Fatalf("RefreshDocument: %v", err)
		}
		if document.Content() != "RETURN 2" || document.Revision() != 1 {
			t.Fatalf("discovered document = revision %d content %q", document.Revision(), document.Content())
		}
	})
}

func TestRefreshDocumentPropagatesWatchRegistrationFailure(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	watcher := workspaceWatcherForTest(t, opened)
	if err := watcher.backend.Close(); err != nil {
		t.Fatalf("close watcher backend: %v", err)
	}

	writeWorkspaceSource(t, root, "nested/query.fql", "RETURN 1")
	if _, err := opened.RefreshDocument(context.Background(), "nested/query.fql"); !errors.Is(err, fsnotify.ErrClosed) {
		t.Fatalf("RefreshDocument error = %v, want fsnotify.ErrClosed", err)
	}
	if _, found := opened.Document("nested/query.fql"); found {
		t.Fatal("document was admitted after watch registration failed")
	}
}

func TestRefreshDocumentSerializesConcurrentCommits(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	writeWorkspaceSource(t, root, "query.fql", "RETURN 2")

	const workers = 16
	var wait sync.WaitGroup
	results := make(chan Document, workers)
	errorsChannel := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			document, err := opened.RefreshDocument(context.Background(), "query.fql")
			results <- document
			errorsChannel <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errorsChannel)

	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("RefreshDocument: %v", err)
		}
	}
	for document := range results {
		if document.Revision() != 2 || document.Content() != "RETURN 2" {
			t.Fatalf("document = revision %d content %q, want revision 2",
				document.Revision(), document.Content())
		}
	}
}

func TestRefreshDocumentSnapshotsCompileIndependently(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	first, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("first RefreshDocument: %v", err)
	}
	writeWorkspaceSource(t, root, "query.fql", "RETURN 2")
	second, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("second RefreshDocument: %v", err)
	}

	firstCompilation, err := opened.CompileDocument(context.Background(), first)
	if err != nil {
		t.Fatalf("compile first: %v", err)
	}
	defer func() { _ = firstCompilation.Close() }()
	secondCompilation, err := opened.CompileDocument(context.Background(), second)
	if err != nil {
		t.Fatalf("compile second: %v", err)
	}
	defer func() { _ = secondCompilation.Close() }()

	if firstCompilation.Source.Revision != 1 || secondCompilation.Source.Revision != 2 {
		t.Fatalf("source revisions = %d and %d, want 1 and 2",
			firstCompilation.Source.Revision, secondCompilation.Source.Revision)
	}
	if got := runCompilation(t, firstCompilation); got != "1" {
		t.Fatalf("first output = %q, want 1", got)
	}
	if got := runCompilation(t, secondCompilation); got != "2" {
		t.Fatalf("second output = %q, want 2", got)
	}
}

func TestRefreshDocumentHonorsCancellationAndClosure(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := opened.RefreshDocument(canceled, "query.fql"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled RefreshDocument error = %v, want context.Canceled", err)
	}
	<-opened.mutationGate
	waiting, stopWaiting := context.WithCancel(context.Background())
	waitResult := make(chan error, 1)
	go func() {
		_, err := opened.RefreshDocument(waiting, "query.fql")
		waitResult <- err
	}()
	stopWaiting()
	select {
	case err := <-waitResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting RefreshDocument error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting RefreshDocument did not observe cancellation")
	}
	opened.mutationGate <- struct{}{}
	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := opened.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed RefreshDocument error = %v, want ErrClosed", err)
	}
}

func TestRefreshAdmissionLosesToWorkspaceCloseDeterministically(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	writeWorkspaceSource(t, root, "created.fql", "RETURN 1")

	<-opened.mutationGate
	refreshContext := newObservedDoneContext(context.Background())
	refreshed := make(chan error, 1)
	go func() {
		_, err := opened.RefreshDocument(refreshContext, "created.fql")
		refreshed <- err
	}()
	<-refreshContext.observed

	entry, owner := manager.beginClose(opened.ID())
	if entry == nil || !owner {
		t.Fatal("beginClose did not acquire workspace close ownership")
	}
	go manager.finishClose(opened.ID(), entry)
	opened.mutationGate <- struct{}{}

	if err := <-refreshed; !errors.Is(err, ErrClosed) {
		t.Fatalf("RefreshDocument error = %v, want ErrClosed", err)
	}
	if err := entry.close.Wait(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, found := opened.Document("created.fql"); found {
		t.Fatal("closing workspace admitted a new source")
	}
}

func runCompilation(t *testing.T, compilation Compilation) string {
	t.Helper()

	session, err := compilation.Plan.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = session.Close() }()
	output, err := session.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	return string(output.Content)
}
