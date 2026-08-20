package exec

import "github.com/MontFerret/ferretd/internal/diagnostic"

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

// Clone returns a deep copy of the snapshot's mutable data.
func (s ExecutionSnapshot) Clone() ExecutionSnapshot {
	result := s
	result.Parameters = cloneParameters(s.Parameters)

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
			result.Failure.Diagnostics[index] = item
			result.Failure.Diagnostics[index].RelatedInformation = append(
				[]diagnostic.RelatedInformation(nil),
				item.RelatedInformation...,
			)
		}
	}

	return result
}
