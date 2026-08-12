package workspace

import (
	"fmt"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
)

func parserPanicError(recovered any) error {
	if err, ok := recovered.(error); ok {
		return fmt.Errorf("parser panic: %w", err)
	}

	return fmt.Errorf("parser panic: %v", recovered)
}

func cloneDiagnostics(values []*ferretdiagnostics.Diagnostic) []*ferretdiagnostics.Diagnostic {
	if len(values) == 0 {
		return nil
	}

	result := make([]*ferretdiagnostics.Diagnostic, 0, len(values))
	for _, value := range values {
		result = append(result, cloneDiagnostic(value))
	}

	return result
}

func cloneDiagnostic(value *ferretdiagnostics.Diagnostic) *ferretdiagnostics.Diagnostic {
	if value == nil {
		return nil
	}

	result := *value
	result.Spans = append([]ferretdiagnostics.ErrorSpan(nil), value.Spans...)

	if value.Source != nil {
		result.Source = ferretsource.New(value.Source.Name(), value.Source.Content())
	}

	return &result
}
