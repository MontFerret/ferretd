package language

import (
	"context"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/source"
)

type (
	// DiagnosticSeverity identifies the importance of a diagnostic.
	DiagnosticSeverity = diagnostic.Severity

	// Diagnostic describes a protocol-neutral source problem.
	Diagnostic = diagnostic.Diagnostic

	// RelatedInformation describes a source location related to a diagnostic.
	RelatedInformation = diagnostic.RelatedInformation

	// DiagnosticReport identifies diagnostics and the exact source snapshot used.
	DiagnosticReport struct {
		Items    []Diagnostic
		Version  *int32
		Snapshot SnapshotID
	}
)

const (
	// DiagnosticSeverityError identifies a compilation error.
	DiagnosticSeverityError = diagnostic.SeverityError
)

// Diagnostics returns diagnostics from the immutable compiler analysis snapshot.
func (s *Service) Diagnostics(ctx context.Context, uri source.URI) (DiagnosticReport, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return DiagnosticReport{}, err
	}

	diagnostics := document.analysis.Diagnostics()
	result := DiagnosticReport{
		Items:    make([]Diagnostic, 0, len(diagnostics)),
		Version:  document.snapshot.version,
		Snapshot: document.snapshot.id,
	}

	for _, diagnostic := range diagnostics {
		result.Items = append(result.Items, convertFerretDiagnostic(uri, document.mapper, diagnostic))
	}

	return result, nil
}
