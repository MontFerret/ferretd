package integration_test

import (
	"context"
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

	runtime, err := uapi.New("test")
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
		"test",
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
	if output == nil || err != nil {
		t.Fatalf("Run = %+v, %v", output, err)
	}

	t.Cleanup(func() { _ = output.Close() })

	content, err := output.Collect(t.Context())
	if content == nil || string(content.Data) != "42" || !errors.Is(err, sessionErr) || !errors.Is(err, planErr) {
		t.Fatalf("Collect = %+v, %v", content, err)
	}

	if err := output.Close(); !errors.Is(err, sessionErr) || !errors.Is(err, planErr) {
		t.Fatalf("Close = %v", err)
	}

	if content.Metadata.ContentType != "application/json" || string(content.Data) != "42" {
		t.Fatalf("detached content after cleanup = %+v", content)
	}

	if sessionCloses.Load() != 1 || planCloses.Load() != 1 {
		t.Fatalf("session/plan closes = %d/%d, want 1/1", sessionCloses.Load(), planCloses.Load())
	}
}

func TestExplicitSessionCollectionPreservesPlanReuse(t *testing.T) {
	var sessionCloses, planCloses atomic.Int64

	runtime, err := uapi.New("test",
		ferret.WithSessionCloseHook(func() error {
			sessionCloses.Add(1)

			return nil
		}),
		ferret.WithPlanCloseHook(func() error {
			planCloses.Add(1)

			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = runtime.Close() })

	plan, err := runtime.Compile(t.Context(), api.NewAnonymousSource("RETURN @value"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = plan.Close() })

	parameters, err := plan.Params(t.Context())
	if err != nil || len(parameters) != 1 || parameters[0] != "value" {
		t.Fatalf("Params = %v, %v", parameters, err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := plan.Params(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Params = %v", err)
	}

	var retained []*api.Content
	for value := 1; value <= 2; value++ {
		session, err := plan.NewSession(t.Context(), api.WithParams(map[string]any{"value": value}))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = session.Close() })

		output, err := session.Run(t.Context())
		if err != nil || output == nil {
			t.Fatalf("Run = %v, %v", output, err)
		}

		t.Cleanup(func() { _ = output.Close() })

		content, err := output.Collect(t.Context())
		if err != nil || content == nil {
			t.Fatalf("Collect = %+v, %v", content, err)
		}

		if err := output.Close(); err != nil {
			t.Fatal(err)
		}

		if sessionCloses.Load() != int64(value-1) || planCloses.Load() != 0 {
			t.Fatal("output cleanup closed caller-owned parents")
		}

		if err := session.Close(); err != nil {
			t.Fatal(err)
		}

		retained = append(retained, content)
	}

	if err := plan.Close(); err != nil {
		t.Fatal(err)
	}

	if sessionCloses.Load() != 2 || planCloses.Load() != 1 || string(retained[0].Data) != "1" || string(retained[1].Data) != "2" {
		t.Fatalf("cleanup counts = %d/%d, content = %+v", sessionCloses.Load(), planCloses.Load(), retained)
	}
}
