package ferretapi

import (
	"testing"

	"github.com/MontFerret/api"
)

func BenchmarkAdapterCompile(b *testing.B) {
	runtime := newTestRuntime(b)
	source := api.NewAnonymousSource("RETURN @value")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		plan, err := runtime.Compile(b.Context(), source)
		if err != nil {
			b.Fatal(err)
		}

		if err := plan.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAdapterCompileUnicode(b *testing.B) {
	runtime := newTestRuntime(b)
	source := api.NewAnonymousSource("LET prefix = \"é\"\nRETURN TEST(prefix)")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		plan, err := runtime.Compile(b.Context(), source)
		if err != nil {
			b.Fatal(err)
		}

		if err := plan.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAdapterSession(b *testing.B) {
	runtime := newTestRuntime(b)

	plan, err := runtime.Compile(b.Context(), api.NewAnonymousSource("RETURN @value"))
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = plan.Close() })
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		session, err := plan.NewSession(b.Context(), api.WithParam("value", 1))
		if err != nil {
			b.Fatal(err)
		}

		if _, err := session.Run(b.Context()); err != nil {
			b.Fatal(err)
		}

		if err := session.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
