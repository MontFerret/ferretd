package integration_test

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MontFerret/api"
	apidiagnostics "github.com/MontFerret/api/diagnostics"
	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferret/v2/uapi"
)

func newTestRuntime(t testing.TB) *uapi.Runtime {
	t.Helper()

	runtime, err := uapi.New()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})

	return runtime
}

func TestCompilationProducesPortableByteCoordinates(t *testing.T) {
	runtime := newTestRuntime(t)
	for _, input := range []struct {
		source api.Source
		line   int
		column int
	}{
		{api.NewSource("buffer://unicode", `RETURN "é" + missing`), 1, 15},
		{api.NewAnonymousSource("LET prefix = \"é\"\nRETURN missing"), 2, 8},
	} {
		plan, err := runtime.Compile(t.Context(), input.source)
		if plan != nil {
			_ = plan.Close()
			t.Fatal("invalid source published a plan")
		}

		var portable apidiagnostics.Diagnostics
		if !errors.As(err, &portable) || len(portable) != 1 {
			t.Fatalf("compiler diagnostics=%+v error=%v", portable, err)
		}

		start := strings.Index(input.source.Content, "missing")
		found := false
		for _, annotation := range portable[0].Annotations {
			if !annotation.Primary {
				continue
			}

			found = true

			if annotation.Range != (api.Range{
				Location: api.Location{SourceName: input.source.Name, Position: api.Position{Line: input.line, Column: input.column}},
				Span:     api.Span{Start: start, End: start + len("missing")},
			}) {
				t.Fatalf("compiler range=%+v for %q", annotation.Range, input.source.Content)
			}
		}

		if !found || portable[0].Source != input.source {
			t.Fatalf("compiler source/primary annotation=%+v", portable[0])
		}
	}
}

func TestUniversalRuntimeRetainsOutputAndCleanupCauses(t *testing.T) {
	sessionErr, planErr := errors.New("session cleanup"), errors.New("plan cleanup")
	var sessionCloses, planCloses atomic.Int64

	runtime, err := uapi.New(
		ferret.WithSessionCloseHook(func() error {
			sessionCloses.Add(1)

			return sessionErr
		}),
		ferret.WithPlanCloseHook(func() error {
			planCloses.Add(1)

			return planErr
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Close() })

	output, err := runtime.Run(t.Context(), api.NewAnonymousSource("RETURN 42"))
	if output == nil || string(output.Content) != "42" || !errors.Is(err, sessionErr) || !errors.Is(err, planErr) {
		t.Fatalf("Run = %+v, %v", output, err)
	}

	if sessionCloses.Load() != 1 || planCloses.Load() != 1 {
		t.Fatalf("session/plan closes = %d/%d, want 1/1", sessionCloses.Load(), planCloses.Load())
	}
}
