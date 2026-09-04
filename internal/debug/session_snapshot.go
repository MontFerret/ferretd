package debug

import (
	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/exec"
)

// SessionSnapshot is an immutable view of one daemon debug Session.
type SessionSnapshot struct {
	ID               SessionID
	ExecutionSession exec.SessionID
	State            State
	Reason           StopReason
	Location         Location
	HitBreakpointIDs []BreakpointID
	Parameters       exec.Parameters
	Options          exec.RuntimeOptions
	Output           *api.Output
	Failure          *exec.RuntimeFailure
}

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows exec.Parameters' recursive container contract.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]BreakpointID(nil), s.HitBreakpointIDs...)
	result.Parameters = s.Parameters.Clone()
	if s.Output != nil {
		result.Output = &api.Output{
			ContentType: s.Output.ContentType,
			Content:     append([]byte(nil), s.Output.Content...),
		}
	}
	result.Failure = s.Failure.Clone()

	return result
}
