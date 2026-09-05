package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/lifecycle"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// session owns one immutable reusable Universal Plan. The child gate orders
// execution-runtime creation before close; its mutex owns debug compilation and
// Plan lease state. No operation holds the Session mutex while waiting on the gate.
type session struct {
	mu sync.Mutex

	id           SessionID
	source       workspace.SourceSnapshot
	parameters   []string
	text         string
	fsRoot       string
	compileDebug func(context.Context) (api.Plan, error)
	plan         api.Plan
	debugPlan    api.Plan

	debugCompileDone   chan struct{}
	debugCompileErr    error
	debugRuntimeDone   chan struct{}
	debugCompiling     bool
	debugCompileFailed bool
	debugRuntimes      int
	children           lifecycle.Gate
}

func newSession(
	id SessionID,
	source workspace.SourceSnapshot,
	plan api.Plan,
	text string,
	fsRoot string,
	compileDebug func(context.Context) (api.Plan, error),
) *session {
	return &session{
		id:           id,
		source:       source,
		parameters:   append([]string(nil), plan.Params()...),
		text:         text,
		fsRoot:       fsRoot,
		compileDebug: compileDebug,
		plan:         plan,
	}
}

func (s *session) snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return SessionSnapshot{
		ID:         s.id,
		Source:     s.source,
		Parameters: append([]string(nil), s.parameters...),
	}
}

func (s *session) acquireDebugRuntimeTarget(ctx context.Context) (runtimeTarget, error) {
	for {
		s.mu.Lock()
		if !s.children.Accepting() {
			s.mu.Unlock()

			return runtimeTarget{}, ErrSessionClosed
		}

		if s.debugPlan != nil {
			target := s.newDebugRuntimeTargetLocked(s.debugPlan)
			s.mu.Unlock()

			return target, nil
		}

		if s.debugCompileFailed {
			err := s.debugCompileErr
			s.mu.Unlock()

			return runtimeTarget{}, err
		}

		if s.debugCompiling {
			done := s.debugCompileDone
			s.mu.Unlock()

			select {
			case <-ctx.Done():
				return runtimeTarget{}, ctx.Err()
			case <-done:
				continue
			}
		}

		s.debugCompiling = true
		s.debugCompileDone = make(chan struct{})
		done := s.debugCompileDone
		compileDebug := s.compileDebug
		s.mu.Unlock()

		plan, err := compileDebug(ctx)
		if err == nil && plan == nil {
			err = errors.New("debug compilation returned no plan")
		}

		s.mu.Lock()
		closing := !s.children.Accepting()
		var target runtimeTarget
		if err == nil && !closing {
			s.debugPlan = plan
			target = s.newDebugRuntimeTargetLocked(plan)
		} else if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.debugCompileErr = err
			s.debugCompileFailed = true
		}
		s.debugCompiling = false
		close(done)
		s.debugCompileDone = nil
		s.mu.Unlock()

		if err != nil || closing {
			if plan != nil {
				err = errors.Join(err, plan.Close())
			}

			if closing {
				err = errors.Join(ErrSessionClosed, err)
			}

			return runtimeTarget{}, err
		}

		return target, nil
	}
}

func (s *session) newDebugRuntimeTargetLocked(plan api.Plan) runtimeTarget {
	if s.debugRuntimes == 0 {
		s.debugRuntimeDone = make(chan struct{})
	}
	s.debugRuntimes++

	return runtimeTarget{
		sessionID: s.id,
		source:    s.source,
		text:      s.text,
		fsRoot:    s.fsRoot,
		plan:      plan,
	}
}

func (s *session) releaseDebugRuntime() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.debugRuntimes == 0 {
		return
	}

	s.debugRuntimes--
	if s.debugRuntimes == 0 {
		close(s.debugRuntimeDone)
		s.debugRuntimeDone = nil
	}
}

func (s *session) runtimeTarget() runtimeTarget {
	return runtimeTarget{
		sessionID: s.id,
		source:    s.source,
		text:      s.text,
		fsRoot:    s.fsRoot,
		plan:      s.plan,
	}
}

func (s *session) beginRuntimeCreate() bool {
	return s.children.BeginCreate()
}

func (s *session) finishRuntimeCreate() {
	s.children.EndCreate()
}

func (s *session) beginClose() bool {
	return s.children.BeginClose()
}

func (s *session) waitForRuntimeCreates() {
	s.children.WaitForCreates()
}

func (s *session) releasePlans() (api.Plan, api.Plan) {
	s.mu.Lock()
	compileDone := s.debugCompileDone
	runtimeDone := s.debugRuntimeDone
	s.mu.Unlock()

	if compileDone != nil {
		<-compileDone
	}

	if runtimeDone != nil {
		<-runtimeDone
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	plan := s.plan
	s.plan = nil
	debugPlan := s.debugPlan
	s.debugPlan = nil

	return plan, debugPlan
}

func (s *session) finishClose(err error) {
	s.children.FinishClose(err)
}

func (s *session) waitClose(ctx context.Context) error {
	return s.children.WaitClose(ctx)
}

func (s *session) closeStarted() bool {
	return !s.children.Accepting()
}

func (s *session) closeUnpublished() error {
	s.mu.Lock()
	plan := s.plan
	s.plan = nil
	s.mu.Unlock()

	if plan == nil {
		return nil
	}

	return plan.Close()
}
