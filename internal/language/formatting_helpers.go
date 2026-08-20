package language

import (
	"errors"

	"github.com/MontFerret/ferret/v2/pkg/diagnostics"
)

func formatterSourceError(err error) bool {
	var diagnostic *diagnostics.Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.Kind != diagnostics.UnexpectedError
	}

	var set *diagnostics.DiagnosticSet
	if errors.As(err, &set) {
		for _, item := range set.Errors() {
			if item.Kind == diagnostics.UnexpectedError {
				return false
			}
		}

		return true
	}

	return false
}
