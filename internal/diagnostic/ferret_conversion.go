package diagnostic

import (
	"strings"

	apisource "github.com/MontFerret/api/source"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferretd/internal/source"
)

// Convert projects one Ferret diagnostic through the supplied source mapper.
func Convert(uri source.URI, mapper *source.Mapper, value *ferretdiagnostics.Diagnostic) Diagnostic {
	if value == nil {
		return Diagnostic{URI: uri, Severity: SeverityError, Source: "ferret"}
	}

	result := Diagnostic{
		URI:      uri,
		Message:  diagnosticMessage(value),
		Severity: SeverityError,
		Source:   "ferret",
		Code:     value.Kind.String(),
	}

	primaryIndex := -1
	for index := range value.Spans {
		if value.Spans[index].Main {
			primaryIndex = index

			break
		}
	}

	if primaryIndex < 0 && len(value.Spans) > 0 {
		primaryIndex = 0
	}

	if primaryIndex >= 0 {
		result.Range = mapper.SpanToRange(apisource.Span(value.Spans[primaryIndex].Span))
	}

	for index, span := range value.Spans {
		if index == primaryIndex {
			continue
		}

		label := span.Label
		if label == "" {
			label = "Related location"
		}

		result.RelatedInformation = append(result.RelatedInformation, RelatedInformation{
			URI:     uri,
			Range:   mapper.SpanToRange(apisource.Span(span.Span)),
			Message: label,
		})
	}

	return result
}

func diagnosticMessage(value *ferretdiagnostics.Diagnostic) string {
	parts := []string{value.Message}
	if value.Hint != "" {
		parts = append(parts, "Hint: "+value.Hint)
	}

	if value.Note != "" {
		parts = append(parts, "Note: "+value.Note)
	}

	return strings.Join(parts, "\n\n")
}
