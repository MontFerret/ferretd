package debug

import (
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/exec"
)

// SessionSnapshot is an immutable view of one daemon debug Session.
type SessionSnapshot struct {
	ID               SessionID
	Session          exec.SessionID
	State            State
	Reason           StopReason
	Location         Location
	HitBreakpointIDs []uint64
	Parameters       map[string]any
	Options          SessionOptions
	Output           *Output
	Failure          *Failure
}

// Clone returns a deep copy of the snapshot's mutable data.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]uint64(nil), s.HitBreakpointIDs...)
	result.Parameters = cloneParameters(s.Parameters)

	if s.Output != nil {
		result.Output = &Output{
			ContentType: s.Output.ContentType,
			Content:     append([]byte(nil), s.Output.Content...),
		}
	}

	if s.Failure != nil {
		result.Failure = &Failure{
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
