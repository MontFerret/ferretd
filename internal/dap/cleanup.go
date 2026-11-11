package dap

import (
	"context"
	"errors"

	"github.com/MontFerret/ferretd/internal/debug"
)

func (s *Server) closeLaunchResources(owned ownedSession, watch debug.Subscription) error {
	if watch.Cancel != nil {
		watch.Cancel()
	}

	ctx := context.Background()
	var result error

	if owned.debug != "" {
		result = errors.Join(result, s.debugs.CloseSession(ctx, owned.debug))
	}

	if owned.session != "" {
		result = errors.Join(result, s.executions.CloseSession(ctx, owned.session))
	}

	if owned.workspace != "" {
		result = errors.Join(result, s.workspaces.Close(ctx, owned.workspace))
	}

	return result
}

func (s *Server) retainRollback(err error) {
	s.stateMu.Lock()
	s.rollbackErr = errors.Join(s.rollbackErr, err)
	s.stateMu.Unlock()
}

func (s *Server) closeOwnedLaunch() error {
	s.stateMu.Lock()
	if s.ownedClosed {
		result := s.ownedCloseErr
		s.stateMu.Unlock()

		return result
	}

	s.ownedClosed = true
	owned, watch := s.owned, s.watch
	stop, done := s.watchStop, s.watchDone
	s.stateMu.Unlock()

	if stop != nil {
		close(stop)
	}

	if watch.Cancel != nil {
		watch.Cancel()
	}

	// Watch completion also acquires eventMu. Never join it while holding that
	// lock or while a request that needs interruption is still executing.
	if done != nil {
		<-done
	}

	s.eventMu.Lock()
	s.initialStop = nil
	s.breakpoints.finish()
	s.invalidateHandles("cleanup")
	s.eventMu.Unlock()
	result := s.closeLaunchResources(owned, watch)
	s.stateMu.Lock()
	s.ownedCloseErr = result
	s.stateMu.Unlock()

	return result
}

func (s *Server) cleanup() error {
	s.cleanupOnce.Do(func() {
		result := s.closeOwnedLaunch()
		ctx := context.Background()
		result = errors.Join(result, s.debugs.Close(ctx))
		result = errors.Join(result, s.executions.Close(ctx))
		result = errors.Join(result, s.workspaces.Clear(ctx))
		result = errors.Join(result, s.runtimeOwner.Close(ctx))
		s.stateMu.Lock()
		s.pendingLaunch = nil
		clear(s.queuedLaunches)
		s.cleanupErr = errors.Join(result, s.rollbackErr)
		s.stateMu.Unlock()
	})

	return s.cleanupErr
}
