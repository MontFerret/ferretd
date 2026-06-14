package language

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/source"
)

func TestDocumentLifecycle(t *testing.T) {
	ctx := context.Background()
	service := New()
	uri := documentURI(t, "query.fql")

	if err := service.OpenDocument(ctx, uri, "ferret", 1, "RETURN 1"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	document, ok := service.GetDocument(ctx, uri)
	if !ok {
		t.Fatal("GetDocument returned false")
	}
	if document.Version != 1 || document.Text != "RETURN 1" {
		t.Fatalf("GetDocument = %#v", document)
	}

	document.Text = "mutated"
	stored, _ := service.GetDocument(ctx, uri)
	if stored.Text != "RETURN 1" {
		t.Fatalf("stored document was mutated through returned copy: %#v", stored)
	}

	if err := service.ChangeDocument(ctx, uri, 2, []TextChange{{Text: "RETURN 2"}, {Text: "RETURN 3"}}); err != nil {
		t.Fatalf("ChangeDocument: %v", err)
	}
	changed, _ := service.GetDocument(ctx, uri)
	if changed.Version != 2 || changed.Text != "RETURN 3" {
		t.Fatalf("changed document = %#v", changed)
	}

	if err := service.OpenDocument(ctx, uri, "ferret", 7, "RETURN 7"); err != nil {
		t.Fatalf("replace OpenDocument: %v", err)
	}
	replaced, _ := service.GetDocument(ctx, uri)
	if replaced.Version != 7 || replaced.Text != "RETURN 7" {
		t.Fatalf("replaced document = %#v", replaced)
	}

	if err := service.CloseDocument(ctx, uri); err != nil {
		t.Fatalf("CloseDocument: %v", err)
	}
	if err := service.CloseDocument(ctx, uri); err != nil {
		t.Fatalf("second CloseDocument: %v", err)
	}
	if _, ok := service.GetDocument(ctx, uri); ok {
		t.Fatal("closed document remains stored")
	}
}

func TestChangeDocumentErrors(t *testing.T) {
	ctx := context.Background()
	service := New()
	uri := documentURI(t, "query.fql")

	if err := service.ChangeDocument(ctx, uri, 1, []TextChange{{Text: "RETURN 1"}}); !errors.Is(err, ErrDocumentNotOpen) {
		t.Fatalf("ChangeDocument missing error = %v", err)
	}
	if err := service.OpenDocument(ctx, uri, "ferret", 2, "RETURN 2"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	if err := service.ChangeDocument(ctx, uri, 2, []TextChange{{Text: "RETURN 3"}}); !errors.Is(err, ErrStaleDocumentVersion) {
		t.Fatalf("ChangeDocument stale error = %v", err)
	}
	if err := service.ChangeDocument(ctx, uri, 3, nil); !errors.Is(err, ErrNoTextChanges) {
		t.Fatalf("ChangeDocument empty error = %v", err)
	}
}

func TestOpenDocumentRejectsNonFileURI(t *testing.T) {
	err := New().OpenDocument(context.Background(), "https://example.com/query.fql", "ferret", 1, "RETURN 1")
	if err == nil {
		t.Fatal("OpenDocument returned nil error")
	}
}

func documentURI(t *testing.T, name string) string {
	t.Helper()

	uri, err := source.PathToURI(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("PathToURI: %v", err)
	}
	return uri
}
