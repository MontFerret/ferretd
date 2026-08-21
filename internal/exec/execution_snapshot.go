package exec

import (
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/params"
)

type (
	// FailureCategory classifies a failed execution phase.
	FailureCategory uint8

	// RuntimeOutput is a defensively copied encoded Ferret result shared by
	// ordinary and debugger execution.
	RuntimeOutput struct {
		ContentType string
		Content     []byte
	}

	// Output is the ordinary Execution name for RuntimeOutput.
	Output = RuntimeOutput

	// RuntimeFailure retains source-aware failure information shared by ordinary
	// and debugger execution.
	RuntimeFailure struct {
		Message     string
		Diagnostics []diagnostic.Diagnostic
	}

	// Failure adds the ordinary execution phase to shared runtime failure details.
	Failure struct {
		Category FailureCategory
		RuntimeFailure
	}

	// ExecutionSnapshot is an immutable view of one daemon Execution.
	ExecutionSnapshot struct {
		ID         ExecutionID
		Session    SessionID
		State      State
		Parameters map[string]any
		Options    ExecutionOptions
		Output     *Output
		Failure    *Failure
	}
)

const (
	// FailureSessionCreation identifies Plan.NewSession failure.
	FailureSessionCreation FailureCategory = iota + 1
	// FailureRuntime identifies Ferret Session.Run failure.
	FailureRuntime
	// FailureCleanup identifies Ferret Session cleanup failure.
	FailureCleanup
)

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows the recursive container contract in internal/params.
func (s ExecutionSnapshot) Clone() ExecutionSnapshot {
	result := s
	result.Parameters = params.Clone(s.Parameters)
	result.Output = s.Output.Clone()
	result.Failure = s.Failure.Clone()

	return result
}

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

// Clone returns an independent copy of the categorized execution failure.
func (f *Failure) Clone() *Failure {
	if f == nil {
		return nil
	}

	details := f.RuntimeFailure.Clone()

	return &Failure{Category: f.Category, RuntimeFailure: *details}
}
