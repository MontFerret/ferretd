// Package diagnostic contains protocol-neutral Ferret diagnostic projections.
package diagnostic

import (
	"errors"
	"iter"
	"strings"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/vm"
	"github.com/MontFerret/ferretd/internal/source"
)

// runtimeErrorSet matches Ferret's aggregate VM error without importing its internal concrete type.
type runtimeErrorSet interface {
	Errors() iter.Seq2[int, *vm.RuntimeError]
}

type (
	// Severity identifies the importance of a diagnostic.
	Severity uint8

	// Diagnostic describes a protocol-neutral source problem.
	Diagnostic struct {
		URI                string
		Message            string
		Severity           Severity
		Range              source.Range
		Source             string
		Code               string
		RelatedInformation []RelatedInformation
	}

	// RelatedInformation describes a source location related to a diagnostic.
	RelatedInformation struct {
		URI     string
		Range   source.Range
		Message string
	}
)

const (
	// SeverityError identifies a compilation or runtime error.
	SeverityError Severity = 1
)

// Convert projects one Ferret diagnostic through the supplied source mapper.
func Convert(uri string, mapper *source.Mapper, value *ferretdiagnostics.Diagnostic) Diagnostic {
	if value == nil {
		return Diagnostic{URI: uri, Severity: SeverityError, Source: "ferret"}
	}

	result := Diagnostic{
		URI:      uri,
		Message:  message(value),
		Severity: SeverityError,
		Source:   "ferret",
		Code:     value.Kind.String(),
	}

	primaryIndex := -1
	for i := range value.Spans {
		if value.Spans[i].Main {
			primaryIndex = i

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

	for i, span := range value.Spans {
		if i == primaryIndex {
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

// FromError extracts Ferret diagnostics from a compilation or runtime error.
func FromError(uri, text string, err error) []Diagnostic {
	values := ferretDiagnostics(err)
	if len(values) == 0 {
		return nil
	}

	mapper := source.NewMapper(text)
	result := make([]Diagnostic, 0, len(values))
	for _, value := range values {
		result = append(result, Convert(uri, mapper, value))
	}

	return result
}

func ferretDiagnostics(err error) []*ferretdiagnostics.Diagnostic {
	var runtimeSet runtimeErrorSet
	if errors.As(err, &runtimeSet) {
		result := make([]*ferretdiagnostics.Diagnostic, 0)
		for _, item := range runtimeSet.Errors() {
			if item != nil && item.Diagnostic != nil {
				result = append(result, item.Diagnostic)
			}
		}

		return result
	}

	var runtimeError *vm.RuntimeError
	if errors.As(err, &runtimeError) && runtimeError != nil && runtimeError.Diagnostic != nil {
		return []*ferretdiagnostics.Diagnostic{runtimeError.Diagnostic}
	}

	var set *ferretdiagnostics.DiagnosticSet
	if errors.As(err, &set) && set != nil {
		result := make([]*ferretdiagnostics.Diagnostic, 0, set.Size())
		for _, item := range set.Errors() {
			result = append(result, item)
		}

		return result
	}

	var single *ferretdiagnostics.Diagnostic
	if errors.As(err, &single) && single != nil {
		return []*ferretdiagnostics.Diagnostic{single}
	}

	return nil
}

func message(value *ferretdiagnostics.Diagnostic) string {
	parts := []string{value.Message}
	if value.Hint != "" {
		parts = append(parts, "Hint: "+value.Hint)
	}

	if value.Note != "" {
		parts = append(parts, "Note: "+value.Note)
	}

	return strings.Join(parts, "\n\n")
}
