package exec

import "github.com/MontFerret/ferretd/internal/diagnostic"

type (
	// RuntimeOutput is a defensively copied encoded Ferret result shared by
	// ordinary and debugger execution.
	RuntimeOutput struct {
		ContentType string
		Content     []byte
	}

	// RuntimeFailure retains source-aware failure information shared by ordinary
	// and debugger execution.
	RuntimeFailure struct {
		Message     string
		Diagnostics []diagnostic.Diagnostic
	}
)

// Clone returns an independent copy of the encoded output.
func (o *RuntimeOutput) Clone() *RuntimeOutput {
	if o == nil {
		return nil
	}

	return &RuntimeOutput{
		ContentType: o.ContentType,
		Content:     append([]byte(nil), o.Content...),
	}
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
