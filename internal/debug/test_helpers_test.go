package debug

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

type debugFixture struct {
	manager    *Manager
	executions *exec.Manager
	workspaces *workspace.Manager
	workspace  *workspace.Workspace
	session    exec.SessionSnapshot
}

func mustNewExecutionManager(t testing.TB, workspaces *workspace.Manager) *exec.Manager {
	t.Helper()

	manager, err := exec.New(workspaces)
	if err != nil {
		t.Fatalf("exec.New: %v", err)
	}

	return manager
}

func mustNewManager(t testing.TB, executions *exec.Manager) *Manager {
	t.Helper()

	manager, err := New(executions)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

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

func newDebugFixture(t *testing.T, query string) debugFixture {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte(query), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	workspaces := workspace.New()
	opened, err := workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("workspace Open: %v", err)
	}
	executions := mustNewExecutionManager(t, workspaces)
	manager := mustNewManager(t, executions)
	session, err := executions.CreateSession(context.Background(), opened.ID(), "query.fql")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close(context.Background())
		_ = executions.Close(context.Background())
		_ = workspaces.Clear(context.Background())
	})

	return debugFixture{
		manager:    manager,
		executions: executions,
		workspaces: workspaces,
		workspace:  opened,
		session:    session,
	}
}
