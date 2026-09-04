package workspace

import (
	"context"
	"os"
	"path/filepath"
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

func writeWorkspaceSource(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
