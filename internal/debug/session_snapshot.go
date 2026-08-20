package debug

import (
	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/params"
)

// Output is the encoded terminal result of a completed debug Session.
type Output struct {
	ContentType string
	Content     []byte
}

// Failure retains durable debugger failure information.
type Failure struct {
	Message     string
	Diagnostics []diagnostic.Diagnostic
}

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

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows the recursive container contract in internal/params.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]uint64(nil), s.HitBreakpointIDs...)
	result.Parameters = params.Clone(s.Parameters)

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
			result.Failure.Diagnostics[index] = item.Clone()
		}
	}

	return result
}
