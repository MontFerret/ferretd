package debug

import (
	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
)

// SessionSnapshot is an immutable view of one daemon debug Session.
type SessionSnapshot struct {
	ID               SessionID
	ExecutionSession exec.SessionID
	State            State
	Reason           apidebugger.Reason
	Location         apisource.Location
	HitBreakpointIDs []apidebugger.BreakpointID
	Parameters       exec.Parameters
	Options          exec.RuntimeOptions
	// Output is detached debugger event content that remains valid after cleanup.
	Output  *api.Content
	Failure *exec.RuntimeFailure
}

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows exec.Parameters' recursive container contract.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]apidebugger.BreakpointID(nil), s.HitBreakpointIDs...)
	result.Parameters = s.Parameters.Clone()
	result.Output = cloneContent(s.Output)
	result.Failure = s.Failure.Clone()

	return result
}

func (d *session) snapshot() SessionSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

func (d *session) snapshotLocked() SessionSnapshot {
	return SessionSnapshot{
		ID:               d.id,
		ExecutionSession: d.runtime.SessionID(),
		State:            d.state,
		Reason:           d.reason,
		Location:         d.location,
		HitBreakpointIDs: append([]apidebugger.BreakpointID(nil), d.hitBreakpointIDs...),
		Parameters:       d.runtime.Parameters(),
		Options:          d.runtime.Options(),
		Output:           cloneContent(d.output),
		Failure:          d.failure.Clone(),
	}
}
