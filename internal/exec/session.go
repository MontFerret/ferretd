package exec

import (
	"context"
	"errors"
	"sync"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/lifecycle"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// session owns its binding and immutable reusable Universal Plans. Its mutex owns
// pending construction, retained rollback errors, debug compilation, and Plan
// leases. The child gate orders execution-runtime creation before close. No
// operation holds the Session mutex while waiting on the gate.
type session struct {
	mu sync.Mutex

	id            SessionID
	acquisitionID string
	source        workspace.SourceSnapshot
	parameters    []string
	text          string
	fsRoot        string
	binding       *runtimes.Binding
	pending       map[*construction]struct{}
	cleanupErr    error
	compileDebug  func(context.Context) (api.Plan, error)
	plan          api.Plan
	debugPlan     api.Plan

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
	parameters []string,
	text string,
	fsRoot string,
	compileDebug func(context.Context) (api.Plan, error),
	binding *runtimes.Binding,
) *session {
	return &session{
		id:           id,
		source:       source,
		parameters:   append([]string(nil), parameters...),
		text:         text,
		fsRoot:       fsRoot,
		compileDebug: compileDebug,
		plan:         plan,
		binding:      binding,
		pending:      make(map[*construction]struct{}),
	}
}

func (s *session) snapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return SessionSnapshot{
		ID:            s.id,
		AcquisitionID: s.acquisitionID,
		RuntimeName:   s.binding.Name(),
		Source:        s.source,
		Text:          s.text,
		Parameters:    append([]string(nil), s.parameters...),
	}
}

func (s *session) acquireDebugRuntimeTarget(ctx context.Context, operation *construction) (runtimeTarget, error) {
	for {
		s.mu.Lock()
		if err := constructionError(ctx); err != nil {
			s.mu.Unlock()

			return runtimeTarget{}, err
		}

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
		err = errors.Join(err, constructionError(ctx))
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
				err = errors.Join(err, operation.recordCleanup(plan.Close()))
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

func (s *session) beginRuntimeCreate(operation *construction) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.children.BeginCreate() {
		return false
	}

	s.pending[operation] = struct{}{}

	return true
}

func (s *session) finishRuntimeCreate(operation *construction) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var closedErr error

	if !s.children.Accepting() {
		s.cleanupErr = errors.Join(s.cleanupErr, operation.cleanupErr)
		closedErr = ErrSessionClosed
	}

	// Retain rollback errors before EndCreate can wake committed teardown.
	delete(s.pending, operation)
	s.children.EndCreate()

	return closedErr
}

func (s *session) beginClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.children.BeginClose()
}

func (s *session) waitForRuntimeCreates() error {
	s.children.WaitForCreates()
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cleanupErr
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
		return s.binding.Release()
	}

	return errors.Join(plan.Close(), s.binding.Release())
}

func (s *session) cancelPending() {
	s.mu.Lock()
	pending := make([]*construction, 0, len(s.pending))
	for operation := range s.pending {
		pending = append(pending, operation)
	}

	s.mu.Unlock()

	for _, operation := range pending {
		operation.stop(ErrSessionClosed)
	}
}

// publicationError is the child publication gate. Once this check succeeds,
// admission keeps the parent alive until the child is registered or handed off.
func (s *session) publicationError(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.children.Accepting() {
		return errors.Join(ErrSessionClosed, constructionError(ctx))
	}

	return constructionError(ctx)
}

func (s *session) prepareInput(parameters Parameters, options RuntimeOptions) (runtimeInput, error) {
	normalized, err := options.normalizedFor(s.binding.Provider())
	if err != nil {
		return runtimeInput{}, err
	}

	return runtimeInput{parameters: parameters.Clone(), options: normalized}, nil
}
