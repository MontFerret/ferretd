package exec

import (
	"sync"

	ferret "github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Session owns one immutable reusable Ferret Plan.
type Session struct {
	mu sync.Mutex

	id         SessionID
	source     workspace.SourceSnapshot
	parameters []string
	text       string
	plan       *ferret.Plan
	executions map[ExecutionID]*Execution
	closing    bool
	closeDone  chan struct{}
	closeErr   error
}

func newSession(
	id SessionID,
	compilation workspace.Compilation,
	text string,
) *Session {
	return &Session{
		id:         id,
		source:     compilation.Source,
		parameters: compilation.Plan.Params(),
		text:       text,
		plan:       compilation.Plan,
		executions: make(map[ExecutionID]*Execution),
		closeDone:  make(chan struct{}),
	}
}

// Snapshot returns an immutable Session view.
func (s *Session) Snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return SessionSnapshot{
		ID:         s.id,
		Source:     s.source,
		Parameters: append([]string(nil), s.parameters...),
	}
}

func (s *Session) addExecution(execution *Execution) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closing {
		return ErrSessionClosed
	}

	s.executions[execution.id] = execution

	return nil
}

func (s *Session) beginClose() ([]*Execution, *ferret.Plan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closing {
		return nil, nil, false
	}

	s.closing = true
	executions := make([]*Execution, 0, len(s.executions))

	for _, execution := range s.executions {
		executions = append(executions, execution)
	}

	s.executions = make(map[ExecutionID]*Execution)
	plan := s.plan
	s.plan = nil

	return executions, plan, true
}

func (s *Session) finishClose(err error) {
	s.mu.Lock()
	s.closeErr = err
	close(s.closeDone)
	s.mu.Unlock()
}

func (s *Session) closeResult() error {
	<-s.closeDone

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closeErr
}
