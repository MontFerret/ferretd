package language

import (
	"context"
	"strings"
	"testing"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

	"github.com/MontFerret/ferretd/internal/source"
)

func TestDiagnostics(t *testing.T) {
	ctx := context.Background()
	service := New()
	uri := documentURI(t, "query.fql")

	if err := service.OpenDocument(ctx, uri, "ferret", 1, "RETURN 1"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	diagnostics, err := service.Diagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("Diagnostics valid: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("valid diagnostics = %#v", diagnostics)
	}

	if err := service.ChangeDocument(ctx, uri, 2, []TextChange{{Text: "RETURN missing"}}); err != nil {
		t.Fatalf("ChangeDocument: %v", err)
	}
	diagnostics, err = service.Diagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("Diagnostics invalid: %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("invalid document produced no diagnostics")
	}
	if diagnostics[0].Source != "ferret" || diagnostics[0].Code == "" {
		t.Fatalf("diagnostic metadata = %#v", diagnostics[0])
	}
	if diagnostics[0].Range.Start.Character == diagnostics[0].Range.End.Character {
		t.Fatalf("diagnostic range is empty: %#v", diagnostics[0].Range)
	}
}

func TestDiagnosticsForEmptyDocument(t *testing.T) {
	ctx := context.Background()
	service := New()
	uri := documentURI(t, "empty.fql")
	if err := service.OpenDocument(ctx, uri, "ferret", 1, ""); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	diagnostics, err := service.Diagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != "SyntaxError" {
		t.Fatalf("empty diagnostics = %#v", diagnostics)
	}
}

func TestConvertFerretDiagnostic(t *testing.T) {
	diagnostic := &ferretdiagnostics.Diagnostic{
		Kind:    "NameError",
		Message: "Variable is not defined",
		Hint:    "Declare it first.",
		Note:    "Names are case-sensitive.",
		Spans: []ferretdiagnostics.ErrorSpan{
			ferretdiagnostics.NewMainErrorSpan(ferretsource.Span{Start: 7, End: 8}, "missing"),
			ferretdiagnostics.NewSecondaryErrorSpan(ferretsource.Span{Start: 0, End: 6}, "related declaration"),
		},
	}

	got := convertFerretDiagnostic("file:///query.fql", source.NewMapper("RETURN x"), diagnostic)
	if !strings.Contains(got.Message, "Hint: Declare it first.") || !strings.Contains(got.Message, "Note: Names are case-sensitive.") {
		t.Fatalf("diagnostic message = %q", got.Message)
	}
	if got.Range != (source.Range{Start: source.Position{Character: 7}, End: source.Position{Character: 8}}) {
		t.Fatalf("diagnostic range = %#v", got.Range)
	}
	if len(got.RelatedInformation) != 1 || got.RelatedInformation[0].Message != "related declaration" {
		t.Fatalf("related information = %#v", got.RelatedInformation)
	}
}
