package lifecycle

import (
	"context"
	"sync"
)

// Gate coordinates child creation against one committed parent close. Its
// zero value accepts creators until BeginClose succeeds. A Gate must not be
// copied after first use.
type Gate struct {
	mu sync.Mutex

	close       CloseOperation
	creating    int
	createsDone chan struct{}
}

// BeginCreate admits one creator unless close has already been committed.
func (g *Gate) BeginCreate() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.close.Started() {
		return false
	}

	if g.creating == 0 {
		g.createsDone = make(chan struct{})
	}

	g.creating++

	return true
}

// EndCreate releases one admitted creator and reports whether no creators
// remain.
func (g *Gate) EndCreate() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.creating == 0 {
		panic("lifecycle: end child creation without admission")
	}

	g.creating--
	if g.creating != 0 {
		return false
	}

	close(g.createsDone)
	g.createsDone = nil

	return true
}

// BeginClose stops future creation and reports whether the caller owns parent
// teardown.
func (g *Gate) BeginClose() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.close.Begin()
}

// Accepting reports whether new creators may still enter.
func (g *Gate) Accepting() bool {
	return !g.close.Started()
}

// Idle reports whether no creator is currently admitted.
func (g *Gate) Idle() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.creating == 0
}

// WaitForCreates waits for every creator admitted before BeginClose. It does
// not accept a context because committed parent teardown must not be abandoned
// when one caller stops waiting.
func (g *Gate) WaitForCreates() {
	g.mu.Lock()
	if !g.close.Started() {
		g.mu.Unlock()

		panic("lifecycle: wait for child creation before close begins")
	}

	done := g.createsDone
	g.mu.Unlock()

	if done != nil {
		<-done
	}
}

// FinishClose publishes the parent teardown result after all admitted creators
// have left the gate.
func (g *Gate) FinishClose(err error) {
	g.mu.Lock()
	if g.creating != 0 {
		g.mu.Unlock()

		panic("lifecycle: finish parent close with active creators")
	}

	g.mu.Unlock()

	g.close.Finish(err)
}

// WaitClose waits for the committed parent teardown result.
func (g *Gate) WaitClose(ctx context.Context) error {
	return g.close.Wait(ctx)
}
