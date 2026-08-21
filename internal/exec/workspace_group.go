package exec

import (
	"context"
	"sync"

	"github.com/MontFerret/ferretd/internal/lifecycle"
)

// workspaceGroup owns Session creation admission and retained membership for
// one workspace. sessionRegistry.mu precedes its mutex when both are needed;
// committed teardown may use the group independently after creation stops.
type workspaceGroup struct {
	mu sync.Mutex

	gate     lifecycle.Gate
	sessions map[SessionID]*sessionEntry
}

func newWorkspaceGroup() *workspaceGroup {
	return &workspaceGroup{sessions: make(map[SessionID]*sessionEntry)}
}

func (g *workspaceGroup) beginCreate() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.gate.BeginCreate()
}

func (g *workspaceGroup) finishCreate() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	idle := g.gate.EndCreate()

	return idle && g.gate.Accepting() && len(g.sessions) == 0
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

func (g *workspaceGroup) waitForCreates() {
	g.gate.WaitForCreates()
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
