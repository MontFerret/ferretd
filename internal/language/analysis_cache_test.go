package language

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestAnalysisCacheCoalescesConcurrentRequests(t *testing.T) {
	service, uri := openLanguageDocument(t, "RETURN 1")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	realAnalyze := service.compiler.Analyze
	service.analyze = func(src *ferretsource.Source) (*compiler.Analysis, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release

		return realAnalyze(src)
	}

	errorsOut := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := service.Diagnostics(context.Background(), uri)
			errorsOut <- err
		}()
	}

	<-started
	close(release)
	for range 2 {
		if err := <-errorsOut; err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("analysis calls = %d, want 1", got)
	}
}

func TestAnalysisCacheCancellationDoesNotCancelAtomicOwner(t *testing.T) {
	service, uri := openLanguageDocument(t, "RETURN 1")
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	realAnalyze := service.compiler.Analyze
	service.analyze = func(src *ferretsource.Source) (*compiler.Analysis, error) {
		close(started)
		<-release
		defer close(completed)

		return realAnalyze(src)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Diagnostics(ctx, uri)
		result <- err
	}()
	<-started
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled diagnostics error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled caller did not return while analysis remained blocked")
	}

	close(release)
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("cache-owned analysis did not finish")
	}

	if _, err := service.Diagnostics(context.Background(), uri); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisCacheLateOldResultCannotReplaceNewSnapshot(t *testing.T) {
	service, uri := openLanguageDocument(t, "RETURN missingOne")
	v1Started := make(chan struct{})
	v2Started := make(chan struct{})
	v1Release := make(chan struct{})
	v2Release := make(chan struct{})
	realAnalyze := service.compiler.Analyze
	service.analyze = func(src *ferretsource.Source) (*compiler.Analysis, error) {
		switch src.Content() {
		case "RETURN missingOne":
			close(v1Started)
			<-v1Release
		case "RETURN 2":
			close(v2Started)
			<-v2Release
		}

		return realAnalyze(src)
	}

	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = service.Diagnostics(context.Background(), uri)
	}()
	<-v1Started

	if err := service.ChangeDocument(context.Background(), uri, 2, []TextChange{{Text: "RETURN 2"}}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer wait.Done()
		_, _ = service.Diagnostics(context.Background(), uri)
	}()
	<-v2Started
	close(v2Release)
	close(v1Release)
	wait.Wait()

	report, err := service.Diagnostics(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	if report.Version == nil || *report.Version != 2 || len(report.Items) != 0 {
		t.Fatalf("current diagnostics = %+v", report)
	}
}

func TestOverlayGenerationRejectsStaleReportsWhenClientVersionIsReused(t *testing.T) {
	service, uri := openLanguageDocument(t, "RETURN missing")
	oldReport, err := service.Diagnostics(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.OpenDocument(context.Background(), uri, 1, "RETURN 1"); err != nil {
		t.Fatal(err)
	}
	if service.IsCurrent(context.Background(), uri, oldReport.Snapshot) {
		t.Fatal("replaced overlay kept the old snapshot current")
	}

	currentReport, err := service.Diagnostics(context.Background(), uri)
	if err != nil {
		t.Fatal(err)
	}
	if currentReport.Version == nil || *currentReport.Version != 1 || len(currentReport.Items) != 0 {
		t.Fatalf("current diagnostics = %+v", currentReport)
	}
}

func TestWorkspaceGenerationRejectsCacheAfterDeleteAndRecreate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	filePath := filepath.Join(root, "query.fql")
	if err := os.WriteFile(filePath, []byte("RETURN missing"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspaces := workspace.New()
	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })
	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	service := mustNewService(t, workspaces, newTestDefaultCatalog(t), Options{})
	uri, err := source.URIFromPath(filePath)
	if err != nil {
		t.Fatal(err)
	}

	oldReport, err := service.Diagnostics(ctx, uri)
	if err != nil || len(oldReport.Items) == 0 {
		t.Fatalf("old diagnostics = %+v, %v", oldReport, err)
	}
	oldDocument, _ := opened.Document("query.fql")

	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.RefreshDocument(ctx, "query.fql"); !errors.Is(err, workspace.ErrDocumentNotFound) {
		t.Fatalf("delete RefreshDocument error = %v", err)
	}
	if _, err := service.Diagnostics(ctx, uri); !errors.Is(err, ErrDocumentNotOpen) {
		t.Fatalf("deleted Diagnostics error = %v, want ErrDocumentNotOpen", err)
	}

	if err := os.WriteFile(filePath, []byte("RETURN 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	newDocument, err := opened.RefreshDocument(ctx, "query.fql")
	if err != nil {
		t.Fatalf("recreate RefreshDocument: %v", err)
	}
	if newDocument.Revision() != oldDocument.Revision() {
		t.Fatalf("recreated revision = %d, want reused public revision %d",
			newDocument.Revision(), oldDocument.Revision())
	}

	newReport, err := service.Diagnostics(ctx, uri)
	if err != nil || len(newReport.Items) != 0 {
		t.Fatalf("new diagnostics = %+v, %v", newReport, err)
	}
	if newReport.Snapshot == oldReport.Snapshot {
		t.Fatal("recreated document reused stale workspace snapshot identity")
	}
}

func BenchmarkAnalysisCold(b *testing.B) {
	service := newTestService(b, Options{})
	uri := source.URI("file:///benchmark.fql")
	var version int32
	for b.Loop() {
		version++
		_ = service.OpenDocument(context.Background(), uri, version, "LET value = 1\nRETURN value")
		_, _ = service.Diagnostics(context.Background(), uri)
	}
}

func BenchmarkAnalysisCacheHit(b *testing.B) {
	service := newTestService(b, Options{})
	uri := source.URI("file:///benchmark.fql")
	_ = service.OpenDocument(context.Background(), uri, 1, "LET value = 1\nRETURN value")
	_, _ = service.Diagnostics(context.Background(), uri)
	b.ResetTimer()

	for b.Loop() {
		_, _ = service.Diagnostics(context.Background(), uri)
	}
}
