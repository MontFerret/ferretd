package exec

import (
	"context"
	"errors"

	"github.com/MontFerret/ferretd/internal/workspace"
)

// CancelSessionAcquisition prevents publication or closes the already published
// Session. The caller can stop waiting independently of committed cleanup.
func (m *Manager) CancelSessionAcquisition(ctx context.Context, id workspace.ID, key string) error {
	if key == "" {
		return errors.New("empty acquisition ID")
	}

	r := m.sessions
	r.mu.Lock()
	group := r.groups[id]
	var a *sessionAcquisition

	if group != nil {
		a = group.acquisitions[key]
	}

	if a == nil {
		// Check membership under the registry lock so workspace teardown cannot
		// remove the group between validation and installing this tombstone.
		if _, err := m.workspaces.Get(context.WithoutCancel(ctx), id); err != nil {
			r.mu.Unlock()

			return err
		}

		if r.closed || (group != nil && !group.accepting()) {
			r.mu.Unlock()

			return workspace.ErrClosed
		}

		if group == nil {
			group = newWorkspaceGroup()
			r.groups[id] = group
		}

		a = group.newAcquisition(key)
		a.settled.Finish(nil)
	}

	first := !a.cancelled
	a.cancelled = true
	operation, entry := a.operation, a.published
	r.mu.Unlock()

	if first && operation != nil {
		operation.stop(context.Canceled)
	}

	if first && entry != nil {
		// Retained entry also joins an ordinary close racing this request.
		return m.closeSession(ctx, entry.session.id, entry)
	}

	return a.settled.Wait(ctx)
}
