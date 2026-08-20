package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/ferret/v2"
	"github.com/MontFerret/ferretd/internal/lifecycle"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Session owns one immutable reusable Ferret Plan. Its mutex precedes the
// embedded close operation when both locks are required for a transition.
type Session struct {
	mu sync.Mutex

	id           SessionID
	source       workspace.SourceSnapshot
	parameters   []string
	text         string
	compileDebug func(context.Context) (workspace.Compilation, error)
	plan         *ferret.Plan
	executions   map[ExecutionID]*Execution
	debugPlan    *ferret.Plan

	debugCompileDone   chan struct{}
	debugCompileErr    error
	debugTargetDone    chan struct{}
	debugCompiling     bool
	debugCompileFailed bool
	debugTargets       int
	close              lifecycle.CloseOperation
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
	}

	if parent != nil {
		result.compileDebug = func(ctx context.Context) (workspace.Compilation, error) {
			return parent.CompileDebugSnapshot(ctx, compilation.Source, text)
		}
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

	if s.close.Started() {
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

func (s *Session) acquireDebugTarget(ctx context.Context) (*DebugTarget, error) {
	for {
		s.mu.Lock()
		if s.close.Started() {
			s.mu.Unlock()

			return nil, ErrSessionClosed
		}

		if s.debugPlan != nil {
			target := s.newDebugTargetLocked(s.debugPlan)
			s.mu.Unlock()

			return target, nil
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
			compileErr := errors.New("debug compilation is unavailable")
			s.mu.Lock()
			s.debugCompiling = false
			s.debugCompileErr = compileErr
			s.debugCompileFailed = true
			close(done)
			s.debugCompileDone = nil
			s.mu.Unlock()

			return nil, compileErr
		}

		compilation, err := compileDebug(ctx)
		if err == nil && compilation.Plan == nil {
			err = errors.New("debug compilation returned no plan")
		}

		if err == nil && compilation.Source != source {
			err = ErrDebugSourceChanged
		}

		s.mu.Lock()
		closing := s.close.Started()
		var target *DebugTarget
		if err == nil && !closing {
			s.debugPlan = compilation.Plan
			target = s.newDebugTargetLocked(compilation.Plan)
		} else if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.debugCompileErr = err
			s.debugCompileFailed = true
		}
		s.debugCompiling = false
		close(done)
		s.debugCompileDone = nil
		s.mu.Unlock()

		if err != nil || closing {
			err = errors.Join(err, compilation.Close())

			if closing {
				err = errors.Join(ErrSessionClosed, err)
			}

			return nil, err
		}

		return target, nil
	}
}

func (s *Session) newDebugTargetLocked(plan *ferret.Plan) *DebugTarget {
	if s.debugTargets == 0 {
		s.debugTargetDone = make(chan struct{})
	}
	s.debugTargets++

	return newDebugTarget(s.id, s.source, s.text, plan, s.releaseDebugTarget)
}

func (s *Session) releaseDebugTarget() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.debugTargets == 0 {
		return
	}

	s.debugTargets--
	if s.debugTargets == 0 {
		close(s.debugTargetDone)
		s.debugTargetDone = nil
	}
}

func (s *Session) beginClose() ([]*Execution, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.close.Begin() {
		return nil, false
	}
	executions := make([]*Execution, 0, len(s.executions))

	// Keep the children registered until their runtime cleanup releases ownership.
	for _, execution := range s.executions {
		executions = append(executions, execution)
	}

	return executions, true
}

func (s *Session) releasePlans() (*ferret.Plan, *ferret.Plan) {
	s.mu.Lock()
	compileDone := s.debugCompileDone
	targetDone := s.debugTargetDone
	s.mu.Unlock()

	if compileDone != nil {
		<-compileDone
	}

	if targetDone != nil {
		<-targetDone
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	plan := s.plan
	s.plan = nil
	debugPlan := s.debugPlan
	s.debugPlan = nil

	return plan, debugPlan
}

func (s *Session) finishClose(err error) {
	s.close.Finish(err)
}
