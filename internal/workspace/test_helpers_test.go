package workspace

import (
	"context"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	manager := New()
	t.Cleanup(func() {
		if err := manager.Clear(context.Background()); err != nil {
			t.Errorf("Clear: %v", err)
		}
	})

	return manager
}
