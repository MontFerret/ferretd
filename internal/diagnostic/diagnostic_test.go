package diagnostic

import (
	"strings"
	"testing"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

	"github.com/MontFerret/ferretd/internal/source"
)

func TestDiagnosticClone(t *testing.T) {
	value := Diagnostic{
		Message: "diagnostic",
		RelatedInformation: []RelatedInformation{{
			Message: "related",
		}},
	}

	cloned := value.Clone()
	cloned.Message = "changed"
	cloned.RelatedInformation[0].Message = "changed"

	if value.Message != "diagnostic" || value.RelatedInformation[0].Message != "related" {
		t.Fatalf("clone mutated original diagnostic: %+v", value)
	}
}

func TestConvert(t *testing.T) {
	value := &ferretdiagnostics.Diagnostic{
		Kind:    "NameError",
		Message: "Variable is not defined",
		Hint:    "Declare it first.",
		Note:    "Names are case-sensitive.",
		Spans: []ferretdiagnostics.ErrorSpan{
			ferretdiagnostics.NewMainErrorSpan(ferretsource.Span{Start: 7, End: 8}, "missing"),
			ferretdiagnostics.NewSecondaryErrorSpan(ferretsource.Span{Start: 0, End: 6}, "related declaration"),
		},
	}

	got := Convert("file:///query.fql", source.NewMapper("RETURN x"), value)
	if !strings.Contains(got.Message, "Hint: Declare it first.") ||
		!strings.Contains(got.Message, "Note: Names are case-sensitive.") {
		t.Fatalf("diagnostic message = %q", got.Message)
	}

	if got.Range != (source.Range{Start: source.Position{Character: 7}, End: source.Position{Character: 8}}) {
		t.Fatalf("diagnostic range = %#v", got.Range)
	}

	if len(got.RelatedInformation) != 1 || got.RelatedInformation[0].Message != "related declaration" {
		t.Fatalf("related information = %#v", got.RelatedInformation)
	}
}
