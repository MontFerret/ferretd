package debug

import (
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/params"
)

type (
	// Output is the debugger Session name for shared runtime output.
	Output = exec.RuntimeOutput

	// Failure is the debugger Session name for shared runtime failure details.
	Failure = exec.RuntimeFailure
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

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows the recursive container contract in internal/params.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]uint64(nil), s.HitBreakpointIDs...)
	result.Parameters = params.Clone(s.Parameters)
	result.Output = s.Output.Clone()
	result.Failure = s.Failure.Clone()

	return result
}
