package language

import (
	"context"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/source"
)

// DiagnosticReport identifies diagnostics and the exact source snapshot used.
type DiagnosticReport struct {
	Items    []diagnostic.Diagnostic
	Version  *int32
	Snapshot SnapshotID
}

// Diagnostics returns diagnostics from the immutable compiler analysis snapshot.
func (s *Service) Diagnostics(ctx context.Context, uri source.URI) (DiagnosticReport, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return DiagnosticReport{}, err
	}

	diagnostics := document.analysis.Diagnostics()
	result := DiagnosticReport{
		Items:    make([]diagnostic.Diagnostic, 0, len(diagnostics)),
		Version:  document.snapshot.version,
		Snapshot: document.snapshot.id,
	}

	for _, item := range diagnostics {
		result.Items = append(result.Items, diagnostic.Convert(uri, document.mapper, item))
	}

	return result, nil
}
