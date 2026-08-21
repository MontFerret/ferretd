package diagnostic

import (
	"errors"
	"iter"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/vm"
	"github.com/MontFerret/ferretd/internal/source"
)

// runtimeErrorSet matches Ferret's aggregate VM error without importing its internal concrete type.
type runtimeErrorSet interface {
	Errors() iter.Seq2[int, *vm.RuntimeError]
}

// FromError extracts Ferret diagnostics from a compilation or runtime error.
func FromError(uri source.URI, text string, err error) []Diagnostic {
	values := extractFerretDiagnostics(err)
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

func extractFerretDiagnostics(err error) []*ferretdiagnostics.Diagnostic {
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
