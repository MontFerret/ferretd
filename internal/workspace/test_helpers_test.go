package workspace

import (
	"context"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	manager := New()
	manager.startWatcher = func(*workspaceWatcher, *Workspace) {}
	t.Cleanup(func() {
		if err := manager.Clear(context.Background()); err != nil {
			t.Errorf("Clear: %v", err)
		}
	})

	return manager
}

func assertPanics(t testing.TB, call func()) {
	t.Helper()

	defer func() {
		if recover() == nil {
			t.Fatal("call did not panic")
		}
	}()

	call()
}
