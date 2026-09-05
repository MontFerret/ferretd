package diagnostic

import (
	"errors"
	"strings"

	apidiagnostics "github.com/MontFerret/api/diagnostics"
	"github.com/MontFerret/ferretd/internal/source"
)

// FromError extracts portable runtime diagnostics from a compilation or runtime error.
func FromError(uri source.URI, text string, err error) []Diagnostic {
	var values apidiagnostics.Diagnostics
	if !errors.As(err, &values) {
		return nil
	}

	result := make([]Diagnostic, len(values))
	for index := range values {
		result[index] = convertAPIDiagnostic(uri, text, values[index])
	}

	return result
}

func convertAPIDiagnostic(uri source.URI, fallbackText string, value apidiagnostics.Diagnostic) Diagnostic {
	text := value.Source.Content
	if text == "" {
		text = fallbackText
	}

	mapper := source.NewMapper(text)
	result := Diagnostic{
		URI:      uri,
		Message:  apiDiagnosticMessage(value),
		Severity: SeverityError,
		Source:   "ferret",
		Code:     value.Kind.String(),
	}

	primaryIndex := -1
	for index := range value.Annotations {
		if value.Annotations[index].Primary {
			primaryIndex = index

			break
		}
	}

	if primaryIndex < 0 && len(value.Annotations) > 0 {
		primaryIndex = 0
	}

	if primaryIndex >= 0 {
		annotation := value.Annotations[primaryIndex]
		result.Range = mapper.SpanToRange(annotation.Range.Span)
	}

	for index, annotation := range value.Annotations {
		if index == primaryIndex {
			continue
		}

		message := annotation.Message
		if message == "" {
			message = "Related location"
		}

		result.RelatedInformation = append(result.RelatedInformation, RelatedInformation{
			URI:     uri,
			Range:   mapper.SpanToRange(annotation.Range.Span),
			Message: message,
		})
	}

	return result
}

func apiDiagnosticMessage(value apidiagnostics.Diagnostic) string {
	parts := []string{value.Message}
	if value.Hint != "" {
		parts = append(parts, "Hint: "+value.Hint)
	}

	if value.Note != "" {
		parts = append(parts, "Note: "+value.Note)
	}

	return strings.Join(parts, "\n\n")
}
