package debug

import (
	"sync"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/lifecycle"
)

// session owns one exec.DebugRuntime and its retained debugger-specific state.
// Its mutex precedes the embedded close operation when both locks are required
// for a transition.
type session struct {
	mu        sync.Mutex
	controlMu sync.Mutex

	id               SessionID
	runtime          *exec.DebugRuntime
	state            State
	reason           apidebugger.Reason
	location         apisource.Location
	hitBreakpointIDs []apidebugger.BreakpointID
	output           *api.Output
	failure          *exec.RuntimeFailure
	breakpoints      map[string][]apidebugger.Breakpoint
	terminating      bool
	close            lifecycle.CloseOperation
	terminalDone     chan struct{}
	sequence         uint64
	lastEvent        Event
	nextWatcher      uint64
	watchers         map[uint64]*debugEventWatcher
}

func newSession(
	id SessionID,
	runtime *exec.DebugRuntime,
) *session {
	result := &session{
		id:           id,
		runtime:      runtime,
		state:        StateCreated,
		breakpoints:  make(map[string][]apidebugger.Breakpoint),
		terminalDone: make(chan struct{}),
		watchers:     make(map[uint64]*debugEventWatcher),
	}
	result.publishLocked(EventCreated, false)

	return result
}

func (d *session) requireInspectable() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == StateRunning {
		return ErrSessionRunning
	}

	if d.state.Terminal() || d.close.Started() {
		return ErrSessionTerminal
	}

	return nil
}

func (d *session) requireStopped() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.state == StateRunning {
		return ErrSessionRunning
	}

	if d.state != StateStopped || d.close.Started() {
		return ErrSessionNotStopped
	}

	return nil
}
