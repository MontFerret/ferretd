package language

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestNewRequiresDependencies(t *testing.T) {
	tests := []struct {
		name string
		new  func() (*Service, error)
		want error
	}{
		{
			name: "workspace manager",
			new:  func() (*Service, error) { return New(nil, runtime.NewFunctions(), Options{}) },
			want: errNilWorkspaceManager,
		},
		{
			name: "functions",
			new:  func() (*Service, error) { return New(workspace.New(), nil, Options{}) },
			want: errNilFunctions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := tt.new()
			if service != nil {
				t.Fatal("New returned a service with a nil dependency")
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("New error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNewUsesSuppliedDependencies(t *testing.T) {
	workspaces := workspace.New()
	service, err := New(workspaces, runtime.NewFunctions(), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if service.workspaces != workspaces {
		t.Fatal("New did not retain the supplied workspace manager")
	}
}

func TestNewDefaultFunctions(t *testing.T) {
	functions, err := NewDefaultFunctions()
	if err != nil {
		t.Fatalf("NewDefaultFunctions: %v", err)
	}
	if functions.Size() == 0 {
		t.Fatal("NewDefaultFunctions returned an empty function registry")
	}
}

func TestDocumentLifecycle(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t, Options{})
	uri := documentURI(t, "query.fql")

	if err := service.OpenDocument(ctx, uri, 1, "RETURN 1"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	document, ok := service.overlay(ctx, uri)
	if !ok {
		t.Fatal("overlay lookup returned false")
	}
	if document.Version != 1 || document.Text != "RETURN 1" {
		t.Fatalf("overlay = %#v", document)
	}

	document.Text = "mutated"
	stored, _ := service.overlay(ctx, uri)
	if stored.Text != "RETURN 1" {
		t.Fatalf("stored document was mutated through returned copy: %#v", stored)
	}

	if err := service.ChangeDocument(ctx, uri, 2, []TextChange{{Text: "RETURN 2"}, {Text: "RETURN 3"}}); err != nil {
		t.Fatalf("ChangeDocument: %v", err)
	}
	changed, _ := service.overlay(ctx, uri)
	if changed.Version != 2 || changed.Text != "RETURN 3" {
		t.Fatalf("changed document = %#v", changed)
	}

	if err := service.OpenDocument(ctx, uri, 7, "RETURN 7"); err != nil {
		t.Fatalf("replace OpenDocument: %v", err)
	}
	replaced, _ := service.overlay(ctx, uri)
	if replaced.Version != 7 || replaced.Text != "RETURN 7" {
		t.Fatalf("replaced document = %#v", replaced)
	}

	if err := service.CloseDocument(ctx, uri); err != nil {
		t.Fatalf("CloseDocument: %v", err)
	}
	if err := service.CloseDocument(ctx, uri); err != nil {
		t.Fatalf("second CloseDocument: %v", err)
	}
	if _, ok := service.overlay(ctx, uri); ok {
		t.Fatal("closed document remains stored")
	}
}

func TestChangeDocumentErrors(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t, Options{})
	uri := documentURI(t, "query.fql")

	if err := service.ChangeDocument(ctx, uri, 1, []TextChange{{Text: "RETURN 1"}}); !errors.Is(err, ErrDocumentNotOpen) {
		t.Fatalf("ChangeDocument missing error = %v", err)
	}
	if err := service.OpenDocument(ctx, uri, 2, "RETURN 2"); err != nil {
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
	err := newTestService(t, Options{}).OpenDocument(
		context.Background(),
		source.URI("https://example.com/query.fql"),
		1,
		"RETURN 1",
	)
	if err == nil {
		t.Fatal("OpenDocument returned nil error")
	}
}

func documentURI(t *testing.T, name string) source.URI {
	t.Helper()

	uri, err := source.URIFromPath(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("URIFromPath: %v", err)
	}
	return uri
}
