package language

import (
	"context"
	"testing"

	"github.com/MontFerret/ferretd/internal/source"
)

var benchmarkServiceSink *Service

func BenchmarkLanguageServiceConstruction(b *testing.B) {
	for b.Loop() {
		benchmarkServiceSink = New(Options{})
	}
}

func BenchmarkRegisteredFunctionCompletion(b *testing.B) {
	service := New(Options{})
	const uri = "file:///benchmark.fql"

	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, "RETURN "); err != nil {
		b.Fatal(err)
	}

	position := source.Position{Character: 7}
	b.ResetTimer()

	for b.Loop() {
		if _, err := service.Completion(context.Background(), uri, position); err != nil {
			b.Fatal(err)
		}
	}
}
