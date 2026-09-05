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
	Output           *api.Output
	Failure          *exec.RuntimeFailure
}

// Clone returns an independent copy of the snapshot's retained mutable data.
// Parameter copying follows exec.Parameters' recursive container contract.
func (s SessionSnapshot) Clone() SessionSnapshot {
	result := s
	result.HitBreakpointIDs = append([]apidebugger.BreakpointID(nil), s.HitBreakpointIDs...)
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

func (d *session) snapshot() SessionSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.snapshotLocked()
}

func (d *session) snapshotLocked() SessionSnapshot {
	result := SessionSnapshot{
		ID:               d.id,
		ExecutionSession: d.runtime.SessionID(),
		State:            d.state,
		Reason:           d.reason,
		Location:         d.location,
		HitBreakpointIDs: append([]apidebugger.BreakpointID(nil), d.hitBreakpointIDs...),
		Parameters:       d.runtime.Parameters(),
		Options:          d.runtime.Options(),
		Output:           d.output,
		Failure:          d.failure.Clone(),
	}

	return result.Clone()
}
