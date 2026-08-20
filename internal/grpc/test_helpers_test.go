package grpc

import (
	"testing"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func mustNewExecutionManager(t testing.TB, workspaces *workspace.Manager) *exec.Manager {
	t.Helper()

	manager, err := exec.New(workspaces)
	if err != nil {
		t.Fatalf("exec.New: %v", err)
	}

	return manager
}

func mustNewServer(
	t testing.TB,
	workspaces *workspace.Manager,
	executions *exec.Manager,
) *Server {
	t.Helper()

	server, err := New(workspaces, executions, "dev", "instance", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return server
}

func mustNewWorkspaceService(t testing.TB, workspaces *workspace.Manager) *workspaceService {
	t.Helper()

	service, err := newWorkspaceService(workspaces)
	if err != nil {
		t.Fatalf("newWorkspaceService: %v", err)
	}

	return service
}
