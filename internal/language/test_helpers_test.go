package language

import (
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/workspace"
)

func newTestService(t testing.TB, options Options) *Service {
	t.Helper()

	return mustNewService(t, workspace.New(), newTestDefaultCatalog(t), options)
}

func mustNewService(
	t testing.TB,
	workspaces *workspace.Manager,
	functions *FunctionCatalog,
	options Options,
) *Service {
	t.Helper()

	service, err := New(workspaces, functions, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return service
}

func newTestDefaultCatalog(t testing.TB) *FunctionCatalog {
	t.Helper()

	functions, warnings, err := NewDefaultFunctionCatalog()
	if err != nil {
		t.Fatalf("NewDefaultFunctionCatalog: %v", err)
	}

	if len(warnings) != 0 {
		t.Fatalf("NewDefaultFunctionCatalog warnings = %+v", warnings)
	}

	return functions
}

func newTestRuntimeCatalog(t testing.TB, functions *runtime.Functions) *FunctionCatalog {
	t.Helper()

	catalog, err := NewRuntimeFunctionCatalog(functions)
	if err != nil {
		t.Fatalf("NewRuntimeFunctionCatalog: %v", err)
	}

	return catalog
}

func testSignatureParameters(names ...string) []SignatureParameter {
	result := make([]SignatureParameter, 0, len(names))
	for _, name := range names {
		result = append(result, SignatureParameter{Name: name, Label: name})
	}

	return result
}
