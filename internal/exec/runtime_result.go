package exec

import "github.com/MontFerret/ferretd/internal/diagnostic"

// RuntimeFailure retains source-aware failure information shared by ordinary
// and debugger execution.
type RuntimeFailure struct {
	Message     string
	Diagnostics []diagnostic.Diagnostic
}

// Clone returns an independent copy of the failure details.
func (f *RuntimeFailure) Clone() *RuntimeFailure {
	if f == nil {
		return nil
	}

	result := &RuntimeFailure{
		Message:     f.Message,
		Diagnostics: make([]diagnostic.Diagnostic, len(f.Diagnostics)),
	}
	for index, item := range f.Diagnostics {
		result.Diagnostics[index] = item.Clone()
	}

	return result
}
