package language

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

	"github.com/MontFerret/ferretd/internal/source"
)

// DiagnosticSeverity identifies the importance of a diagnostic.
type DiagnosticSeverity uint8

const (
	// DiagnosticSeverityError identifies a compilation error.
	DiagnosticSeverityError DiagnosticSeverity = 1
)

// Diagnostic describes a protocol-neutral source problem.
type Diagnostic struct {
	Message            string
	Severity           DiagnosticSeverity
	Range              source.Range
	Source             string
	Code               string
	RelatedInformation []RelatedInformation
}

// RelatedInformation describes a source location related to a diagnostic.
type RelatedInformation struct {
	URI     string
	Range   source.Range
	Message string
}

// Diagnostics compiles an open document snapshot and returns its diagnostics.
func (s *Service) Diagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	document, ok := s.GetDocument(ctx, uri)
	if !ok {
		return nil, fmtDocumentNotOpen(uri)
	}

	_, err := s.compiler.Compile(ferretsource.New(document.Path, document.Text))
	if err == nil {
		return []Diagnostic{}, nil
	}

	mapper := source.NewMapper(document.Text)
	diagnostics := extractFerretDiagnostics(err)
	if len(diagnostics) == 0 {
		return []Diagnostic{{
			Message:  err.Error(),
			Severity: DiagnosticSeverityError,
			Source:   "ferret",
			Code:     ferretdiagnostics.UnexpectedError.String(),
		}}, nil
	}

	result := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		result = append(result, convertFerretDiagnostic(uri, mapper, diagnostic))
	}

	return result, nil
}

func fmtDocumentNotOpen(uri string) error {
	return fmt.Errorf("%w: %s", ErrDocumentNotOpen, uri)
}

func extractFerretDiagnostics(err error) []*ferretdiagnostics.Diagnostic {
	switch typed := err.(type) {
	case *ferretdiagnostics.Diagnostic:
		return []*ferretdiagnostics.Diagnostic{typed}
	case *ferretdiagnostics.DiagnosticSet:
		result := make([]*ferretdiagnostics.Diagnostic, 0, typed.Size())
		for _, diagnostic := range typed.Errors() {
			result = append(result, diagnostic)
		}
		return result
	}

	var diagnostic *ferretdiagnostics.Diagnostic
	if errors.As(err, &diagnostic) {
		return []*ferretdiagnostics.Diagnostic{diagnostic}
	}

	return nil
}

func convertFerretDiagnostic(uri string, mapper *source.Mapper, diagnostic *ferretdiagnostics.Diagnostic) Diagnostic {
	result := Diagnostic{
		Message:  diagnosticMessage(diagnostic),
		Severity: DiagnosticSeverityError,
		Source:   "ferret",
		Code:     diagnostic.Kind.String(),
	}

	var primary *ferretdiagnostics.ErrorSpan
	for i := range diagnostic.Spans {
		span := &diagnostic.Spans[i]
		if span.Main {
			primary = span
			break
		}
	}
	if primary == nil && len(diagnostic.Spans) > 0 {
		primary = &diagnostic.Spans[0]
	}
	if primary != nil {
		result.Range = mapper.SpanToRange(toSourceSpan(primary.Span))
	}

	for _, span := range diagnostic.Spans {
		if span.Main {
			continue
		}

		message := span.Label
		if message == "" {
			message = "Related location"
		}
		result.RelatedInformation = append(result.RelatedInformation, RelatedInformation{
			URI:     uri,
			Range:   mapper.SpanToRange(toSourceSpan(span.Span)),
			Message: message,
		})
	}

	return result
}

func diagnosticMessage(diagnostic *ferretdiagnostics.Diagnostic) string {
	parts := []string{diagnostic.Message}
	if diagnostic.Hint != "" {
		parts = append(parts, "Hint: "+diagnostic.Hint)
	}
	if diagnostic.Note != "" {
		parts = append(parts, "Note: "+diagnostic.Note)
	}
	return strings.Join(parts, "\n\n")
}

func toSourceSpan(span ferretsource.Span) source.Span {
	return source.Span{Start: span.Start, End: span.End}
}
