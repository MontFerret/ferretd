// Package diagnostic contains protocol-neutral Ferret diagnostic projections.
package diagnostic

import (
	"github.com/MontFerret/ferretd/internal/source"
)

type (
	// Severity identifies the importance of a diagnostic.
	Severity uint8

	// Diagnostic describes a protocol-neutral source problem.
	Diagnostic struct {
		URI                source.URI
		Message            string
		Severity           Severity
		Range              source.Range
		Source             string
		Code               string
		RelatedInformation []RelatedInformation
	}

	// RelatedInformation describes a source location related to a diagnostic.
	RelatedInformation struct {
		URI     source.URI
		Range   source.Range
		Message string
	}
)

const (
	// SeverityError identifies a compilation or runtime error.
	SeverityError Severity = 1
)

// Clone returns an independent copy of the diagnostic's mutable data.
func (d Diagnostic) Clone() Diagnostic {
	result := d
	result.RelatedInformation = append([]RelatedInformation(nil), d.RelatedInformation...)

	return result
}
