package language

import (
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/workspace"
)

func newTestService(t testing.TB, options Options) *Service {
	t.Helper()

	return mustNewService(t, workspace.New(), newTestDefaultFunctions(t), options)
}

func mustNewService(
	t testing.TB,
	workspaces *workspace.Manager,
	functions *runtime.Functions,
	options Options,
) *Service {
	t.Helper()

	service, err := New(workspaces, functions, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return service
}

func newTestDefaultFunctions(t testing.TB) *runtime.Functions {
	t.Helper()

	functions, err := NewDefaultFunctions()
	if err != nil {
		t.Fatalf("NewDefaultFunctions: %v", err)
	}

	return functions
}
