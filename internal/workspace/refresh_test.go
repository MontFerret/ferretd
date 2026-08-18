package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	unavailable, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("missing RefreshDocument: %v", err)
	}
	if unavailable.Revision() != 4 || unavailable.Loaded() || len(unavailable.Diagnostics()) == 0 {
		t.Fatalf("unavailable document = revision %d loaded %t diagnostics %d",
			unavailable.Revision(), unavailable.Loaded(), len(unavailable.Diagnostics()))
	}

	stillUnavailable, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("repeated missing RefreshDocument: %v", err)
	}
	if stillUnavailable.Revision() != 4 || stillUnavailable.Loaded() {
		t.Fatalf("repeated unavailable document = revision %d loaded %t",
			stillUnavailable.Revision(), stillUnavailable.Loaded())
	}

	writeWorkspaceSource(t, root, "query.fql", "RETURN 3")
	restored, err := opened.RefreshDocument(context.Background(), "query.fql")
	if err != nil {
		t.Fatalf("restored RefreshDocument: %v", err)
	}
	if restored.Revision() != 5 || !restored.Loaded() || restored.Content() != "RETURN 3" {
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

		document, err := opened.RefreshDocument(context.Background(), "query.fql")
		if err != nil {
			t.Fatalf("RefreshDocument: %v", err)
		}
		if document.Loaded() || document.Revision() != 2 {
			t.Fatalf("document = revision %d loaded %t, want unavailable revision 2",
				document.Revision(), document.Loaded())
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

		document, err := opened.RefreshDocument(context.Background(), "query.fql")
		if err != nil {
			t.Fatalf("RefreshDocument: %v", err)
		}
		if document.Loaded() || document.Revision() != 2 {
			t.Fatalf("document = revision %d loaded %t, want unavailable revision 2",
				document.Revision(), document.Loaded())
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

		if _, err := opened.RefreshDocument(context.Background(), "new.fql"); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("RefreshDocument error = %v, want ErrDocumentNotFound", err)
		}
	})
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
	defer func() { _ = firstCompilation.Plan.Close() }()
	secondCompilation, err := opened.CompileDocument(context.Background(), second)
	if err != nil {
		t.Fatalf("compile second: %v", err)
	}
	defer func() { _ = secondCompilation.Plan.Close() }()

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
	<-opened.refreshGate
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
	opened.refreshGate <- struct{}{}
	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := opened.RefreshDocument(context.Background(), "query.fql"); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed RefreshDocument error = %v, want ErrClosed", err)
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
