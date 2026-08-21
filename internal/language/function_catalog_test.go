package language

import (
	"context"
	"reflect"
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

func TestFunctionCatalogPreservesRegistryMetadata(t *testing.T) {
	library := runtime.NewLibrary()
	library.Namespace("ZeTa").Function().A0().Add("Last", func(context.Context) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("CuStOm").Function().A1().Add("DoThing", func(context.Context, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("CuStOm").Function().A3().Add("DoThing", func(context.Context, runtime.Value, runtime.Value, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("CuStOm").Function().Var().Add("DoThing", func(context.Context, ...runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	functions, err := library.Build()
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := NewRuntimeFunctionCatalog(functions)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.ordered) != 2 {
		t.Fatalf("ordered functions = %+v", catalog.ordered)
	}

	if got, want := catalog.ordered[0].name, "CuStOm::DoThing"; got != want {
		t.Fatalf("first presentation name = %q, want %q", got, want)
	}

	function, ok := catalog.lookup("CUSTOM::dOtHiNg")
	if !ok {
		t.Fatal("case-insensitive lookup failed")
	}

	if function.name != "CuStOm::DoThing" || function.identity != "custom::dothing" ||
		!reflect.DeepEqual(function.arities, []int{1, 3}) || !function.runtimeVariadic {
		t.Fatalf("indexed function = %+v", function)
	}

	if _, ok := catalog.lookup("custom::missing"); ok {
		t.Fatal("missing lookup succeeded")
	}
}
