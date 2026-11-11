package dap

import (
	"errors"

	"github.com/MontFerret/ferretd/internal/exec"
)

// Domain construction can roll back before returning any resource to DAP. Keep
// those close failures without making an ordinary, retryable preparation error
// poison the final adapter close result.
func preparationCleanup(err error) error {
	switch cause := err.(type) { //nolint:errorlint // Visit every unwrap branch to retain all cleanup causes, not only the first match.
	case *exec.PreparationCleanupError:
		return cause
	case interface{ Unwrap() []error }:
		var result error
		for _, child := range cause.Unwrap() {
			result = errors.Join(result, preparationCleanup(child))
		}

		return result
	case interface{ Unwrap() error }:
		return preparationCleanup(cause.Unwrap())
	default:
		return nil
	}
}
