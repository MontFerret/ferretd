package exec

import (
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/params"
)

// FailureCategory classifies a failed execution phase.
type FailureCategory uint8

// Output is a defensively copied encoded Ferret result.
type Output struct {
	ContentType string
	Content     []byte
}

// Failure retains useful execution failure information.
type Failure struct {
	Category    FailureCategory
	Message     string
	Diagnostics []diagnostic.Diagnostic
}

const (
	// FailureSessionCreation identifies Plan.NewSession failure.
	FailureSessionCreation FailureCategory = iota + 1
	// FailureRuntime identifies Ferret Session.Run failure.
	FailureRuntime
	// FailureCleanup identifies Ferret Session cleanup failure.
	FailureCleanup
)

// ExecutionSnapshot is an immutable view of one daemon Execution.
type ExecutionSnapshot struct {
	ID         ExecutionID
	Session    SessionID
	State      State
	Parameters map[string]any
	Options    ExecutionOptions
	Output     *Output
	Failure    *Failure
}

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows the recursive container contract in internal/params.
func (s ExecutionSnapshot) Clone() ExecutionSnapshot {
	result := s
	result.Parameters = params.Clone(s.Parameters)

	if s.Output != nil {
		result.Output = &Output{
			ContentType: s.Output.ContentType,
			Content:     append([]byte(nil), s.Output.Content...),
		}
	}

	if s.Failure != nil {
		result.Failure = &Failure{
			Category:    s.Failure.Category,
			Message:     s.Failure.Message,
			Diagnostics: make([]diagnostic.Diagnostic, len(s.Failure.Diagnostics)),
		}

		for index, item := range s.Failure.Diagnostics {
			result.Failure.Diagnostics[index] = item.Clone()
		}
	}

	return result
}
