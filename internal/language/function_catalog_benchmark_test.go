package language

import (
	"context"
	"testing"

	"github.com/MontFerret/ferretd/internal/source"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func BenchmarkDefaultFunctionCatalog(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		catalog, warnings, err := NewDefaultFunctionCatalog()
		if err != nil {
			b.Fatal(err)
		}
		if len(warnings) != 0 || len(catalog.ordered) == 0 {
			b.Fatalf("catalog = %+v, warnings = %+v", catalog, warnings)
		}
	}
}

func BenchmarkRegisteredFunctionCompletion(b *testing.B) {
	catalog, warnings, err := NewDefaultFunctionCatalog()
	if err != nil {
		b.Fatal(err)
	}
	if len(warnings) != 0 {
		b.Fatalf("catalog warnings = %+v", warnings)
	}

	service, err := New(workspace.New(), catalog, Options{})
	if err != nil {
		b.Fatal(err)
	}

	uri := documentURI(b, "completion-benchmark.fql")
	if err := service.OpenDocument(context.Background(), uri, 1, "RETURN ab"); err != nil {
		b.Fatal(err)
	}
	position := source.Position{Character: 9}
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		items, err := service.Completion(context.Background(), uri, position)
		if err != nil {
			b.Fatal(err)
		}
		if len(items) == 0 {
			b.Fatal("completion returned no items")
		}
	}
}
