package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Session owns one immutable reusable Ferret Plan.
type Session struct {
	mu sync.Mutex

	id           SessionID
	source       workspace.SourceSnapshot
	parameters   []string
	text         string
	compileDebug func(context.Context, string) (workspace.Compilation, error)
	plan         *ferret.Plan
	executions   map[ExecutionID]*Execution
	debugs       map[DebugSessionID]*DebugSession
	debugPlan    *ferret.Plan

	debugCompileDone   chan struct{}
	debugCompileErr    error
	debugCreateDone    chan struct{}
	debugCompiling     bool
	debugCompileFailed bool
	debugCreating      int
	closing            bool
	closeDone          chan struct{}
	closeErr           error
}

func newSession(
	id SessionID,
	parent *workspace.Workspace,
	compilation workspace.Compilation,
	text string,
) *Session {
	result := &Session{
		id:         id,
		source:     compilation.Source,
		parameters: compilation.Plan.Params(),
		text:       text,
		plan:       compilation.Plan,
		executions: make(map[ExecutionID]*Execution),
		debugs:     make(map[DebugSessionID]*DebugSession),
		closeDone:  make(chan struct{}),
	}

	if parent != nil {
		result.compileDebug = parent.CompileDebug
	}

	return result
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

func (s *Session) removeExecution(execution *Execution) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.executions[execution.id] == execution {
		delete(s.executions, execution.id)
	}
}

func (s *Session) beginDebugCreate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closing {
		return ErrSessionClosed
	}

	if s.debugCreating == 0 {
		s.debugCreateDone = make(chan struct{})
	}

	s.debugCreating++

	return nil
}

func (s *Session) finishDebugCreate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.debugCreating--
	if s.debugCreating == 0 {
		close(s.debugCreateDone)
		s.debugCreateDone = nil
	}
}

func (s *Session) debugCompilation(ctx context.Context) (*ferret.Plan, error) {
	for {
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()

			return nil, ErrSessionClosed
		}

		if s.debugPlan != nil {
			plan := s.debugPlan
			s.mu.Unlock()

			return plan, nil
		}

		if s.debugCompileFailed {
			err := s.debugCompileErr
			s.mu.Unlock()

			return nil, err
		}

		if s.debugCompiling {
			done := s.debugCompileDone
			s.mu.Unlock()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}

		s.debugCompiling = true
		s.debugCompileDone = make(chan struct{})
		done := s.debugCompileDone
		compileDebug := s.compileDebug
		source := s.source
		s.mu.Unlock()

		if compileDebug == nil {
			s.mu.Lock()
			s.debugCompiling = false
			s.debugCompileErr = errors.New("debug compilation is unavailable")
			s.debugCompileFailed = true
			close(done)
			s.debugCompileDone = nil
			s.mu.Unlock()

			return nil, s.debugCompileErr
		}

		compilation, err := compileDebug(ctx, source.RelativePath)
		if err == nil && compilation.Plan == nil {
			err = errors.New("debug compilation returned no plan")
		}

		if err == nil && compilation.Source != source {
			err = ErrDebugSourceChanged
		}

		s.mu.Lock()
		closing := s.closing
		if err == nil && !closing {
			s.debugPlan = compilation.Plan
		} else if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.debugCompileErr = err
			s.debugCompileFailed = true
		}
		s.debugCompiling = false
		close(done)
		s.debugCompileDone = nil
		s.mu.Unlock()

		if err != nil || closing {
			if compilation.Plan != nil {
				err = errors.Join(err, compilation.Plan.Close())
			}

			if closing {
				err = errors.Join(ErrSessionClosed, err)
			}

			return nil, err
		}

		return compilation.Plan, nil
	}
}

func (s *Session) addDebugSession(debugSession *DebugSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closing {
		return ErrSessionClosed
	}

	s.debugs[debugSession.id] = debugSession

	return nil
}

func (s *Session) removeDebugSession(debugSession *DebugSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.debugs[debugSession.id] == debugSession {
		delete(s.debugs, debugSession.id)
	}
}

func (s *Session) beginClose() ([]*Execution, []*DebugSession, *ferret.Plan, *ferret.Plan, bool) {
	s.mu.Lock()

	if s.closing {
		s.mu.Unlock()

		return nil, nil, nil, nil, false
	}

	s.closing = true
	compileDone := s.debugCompileDone
	createDone := s.debugCreateDone
	s.mu.Unlock()

	if compileDone != nil {
		<-compileDone
	}

	if createDone != nil {
		<-createDone
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	executions := make([]*Execution, 0, len(s.executions))
	debugSessions := make([]*DebugSession, 0, len(s.debugs))

	// Keep the children registered until their runtime cleanup releases ownership.
	for _, execution := range s.executions {
		executions = append(executions, execution)
	}

	for _, debugSession := range s.debugs {
		debugSessions = append(debugSessions, debugSession)
	}

	plan := s.plan
	s.plan = nil
	debugPlan := s.debugPlan
	s.debugPlan = nil

	return executions, debugSessions, plan, debugPlan, true
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
