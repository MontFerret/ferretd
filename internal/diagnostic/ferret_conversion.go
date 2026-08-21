package diagnostic

import (
	"strings"

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
		result.Range = mapper.SpanToRange(source.Span{
			Start: value.Spans[primaryIndex].Span.Start,
			End:   value.Spans[primaryIndex].Span.End,
		})
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
			URI: uri,
			Range: mapper.SpanToRange(source.Span{
				Start: span.Span.Start,
				End:   span.Span.End,
			}),
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
