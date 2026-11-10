package grpc

import (
	"context"
	"testing"

	"github.com/MontFerret/api"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func mustNewExecutionManager(t testing.TB, workspaces *workspace.Manager) *exec.Manager {
	t.Helper()

	runtime := &unusedRuntime{t: t}

	manager, err := exec.New(workspaces, newTestOwner(t, runtime))
	if err != nil {
		t.Fatalf("exec.New: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close(context.Background())
	})

	return manager
}

func mustNewServer(
	t testing.TB,
	workspaces *workspace.Manager,
	executions *exec.Manager,
) *Server {
	t.Helper()

	server, err := New(workspaces, executions, mustNewRuntimeCatalog(t), "dev", "instance", func() {}, Options{})
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

	service, err := newDaemonService(version, instanceID, shutdown, mustNewRuntimeCatalog(t))
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

func newTestOwner(t testing.TB, runtime api.Runtime) *runtimes.Owner {
	t.Helper()

	catalog, err := runtimes.NewCatalog(runtimes.Config{})
	if err != nil {
		t.Fatal(err)
	}

	owner, err := runtimes.NewOwner(catalog, runtime)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = owner.Close(context.Background()) })

	return owner
}
