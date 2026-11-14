package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferretd/internal/lifecycle"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// workspaceGroup owns Session creation admission and retained membership for
// one workspace. sessionRegistry.mu precedes its mutex when both are needed;
// committed teardown may use the group independently after creation stops.
type workspaceGroup struct {
	mu sync.Mutex

	gate lifecycle.Gate
	// acquisitions is protected by sessionRegistry.mu. Cancelled keys survive until workspace close.
	acquisitions map[string]*sessionAcquisition
	sessions     map[SessionID]*sessionEntry
	pending      map[*construction]struct{}
	cleanupErr   error
}

func newWorkspaceGroup() *workspaceGroup {
	return &workspaceGroup{sessions: make(map[SessionID]*sessionEntry), pending: make(map[*construction]struct{})}
}

func (g *workspaceGroup) beginCreate(operation *construction) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.gate.BeginCreate() {
		return false
	}

	g.pending[operation] = struct{}{}

	return true
}

func (g *workspaceGroup) finishCreate(operation *construction) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var closedErr error

	if !g.gate.Accepting() {
		g.cleanupErr = errors.Join(g.cleanupErr, operation.cleanupErr)
		closedErr = workspace.ErrClosed
	}

	// Retain rollback errors before EndCreate can wake committed teardown.
	delete(g.pending, operation)
	idle := g.gate.EndCreate()

	return idle && closedErr == nil && len(g.sessions) == 0, closedErr
}

func (g *workspaceGroup) add(entry *sessionEntry) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.gate.Accepting() {
		return false
	}

	g.sessions[entry.session.id] = entry

	return true
}

func (g *workspaceGroup) accepting() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.gate.Accepting()
}

func (g *workspaceGroup) removeCompleted(entry *sessionEntry) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.gate.Accepting() && g.sessions[entry.session.id] == entry {
		delete(g.sessions, entry.session.id)
	}

	return g.gate.Accepting() && g.gate.Idle() && len(g.sessions) == 0
}

func (g *workspaceGroup) beginClose() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.gate.BeginClose()
}

func (g *workspaceGroup) waitForCreates() error {
	g.gate.WaitForCreates()
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.cleanupErr
}

func (g *workspaceGroup) retainedSessions() []*sessionEntry {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := make([]*sessionEntry, 0, len(g.sessions))
	for _, entry := range g.sessions {
		result = append(result, entry)
	}

	return result
}

func (g *workspaceGroup) finishClose(err error) {
	g.gate.FinishClose(err)
}

func (g *workspaceGroup) waitClose(ctx context.Context) error {
	return g.gate.WaitClose(ctx)
}

func (g *workspaceGroup) cancelPending() {
	g.mu.Lock()
	pending := make([]*construction, 0, len(g.pending))
	for operation := range g.pending {
		pending = append(pending, operation)
	}

	g.mu.Unlock()

	for _, operation := range pending {
		operation.stop(workspace.ErrClosed)
	}
}

// newAcquisition is called with the Session registry lock held.
func (g *workspaceGroup) newAcquisition(id string) *sessionAcquisition {
	if g.acquisitions == nil {
		g.acquisitions = make(map[string]*sessionAcquisition)
	}

	a := &sessionAcquisition{}
	a.settled.Begin()
	g.acquisitions[id] = a

	return a
}
