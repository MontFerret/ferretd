package ferretapi

import apidiagnostics "github.com/MontFerret/api/diagnostics"

type diagnosticError struct {
	cause       error
	diagnostics apidiagnostics.Diagnostics
}

func newDiagnosticError(cause error, diagnostics apidiagnostics.Diagnostics) error {
	return &diagnosticError{cause: cause, diagnostics: diagnostics}
}

func (e *diagnosticError) Error() string {
	return e.cause.Error()
}

func (e *diagnosticError) Unwrap() []error {
	return []error{e.cause, e.diagnostics}
}
