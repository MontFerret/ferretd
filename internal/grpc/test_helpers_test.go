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

	server, err := New(workspaces, executions, "dev", "instance", func() {})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return server
}

func mustNewDaemonService(
	t testing.TB,
	version string,
	instanceID string,
	shutdown func(),
) *daemonService {
	t.Helper()

	service, err := newDaemonService(version, instanceID, shutdown)
	if err != nil {
		t.Fatalf("newDaemonService: %v", err)
	}

	return service
}

func mustNewWorkspaceService(t testing.TB, workspaces *workspace.Manager) *workspaceService {
	t.Helper()

	service, err := newWorkspaceService(workspaces)
	if err != nil {
		t.Fatalf("newWorkspaceService: %v", err)
	}

	return service
}
