package debug

import (
	"github.com/MontFerret/ferretd/internal/exec"
)

type (
	// SessionSnapshot is an immutable view of one daemon debug Session.
	SessionSnapshot struct {
		ID               SessionID
		ExecutionSession exec.SessionID
		State            State
		Reason           StopReason
		Location         Location
		HitBreakpointIDs []BreakpointID
		Parameters       exec.Parameters
		Options          exec.RuntimeOptions
		Output           *exec.RuntimeOutput
		Failure          *exec.RuntimeFailure
	}
)

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows exec.Parameters' recursive container contract.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]BreakpointID(nil), s.HitBreakpointIDs...)
	result.Parameters = s.Parameters.Clone()
	result.Output = s.Output.Clone()
	result.Failure = s.Failure.Clone()

	return result
}
