package language

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/specs/pkg/api"

	stdlibref "github.com/MontFerret/ferretd/internal/language/stdlib"
)

func TestFunctionIndexPreservesRegistryMetadata(t *testing.T) {
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

	index := newFunctionIndex(functions, nil)
	if len(index.ordered) != 2 {
		t.Fatalf("ordered functions = %+v", index.ordered)
	}

	if got, want := index.ordered[0].name, "CuStOm::DoThing"; got != want {
		t.Fatalf("first presentation name = %q, want %q", got, want)
	}

	function, ok := index.lookup("CUSTOM::dOtHiNg")
	if !ok {
		t.Fatal("case-insensitive lookup failed")
	}

	if function.name != "CuStOm::DoThing" || function.namespace != "CuStOm" || function.identity != "custom::dothing" || len(function.signatures) != 3 ||
		function.signatures[0].Label != "CuStOm::DoThing(arg1)" ||
		function.signatures[1].Label != "CuStOm::DoThing(arg1, arg2, arg3)" ||
		function.signatures[2].Label != "CuStOm::DoThing(arg1...)" || !function.signatures[2].Variadic {
		t.Fatalf("indexed function = %+v", function)
	}

	if _, ok := index.lookup("custom::missing"); ok {
		t.Fatal("missing lookup succeeded")
	}
}

func TestFunctionIndexEnrichesOnlyMatchingRuntimeFunctionsAndSignatures(t *testing.T) {
	library := runtime.NewLibrary()
	library.Namespace("SaMpLe").Function().A1().Add("DoThing", func(context.Context, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("SaMpLe").Function().A2().Add("DoThing", func(context.Context, runtime.Value, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("SaMpLe").Function().Var().Add("DoThing", func(context.Context, ...runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("SaMpLe").Function().A1().Add("Old", func(context.Context, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	functions, err := library.Build()
	if err != nil {
		t.Fatal(err)
	}

	reference := parseFunctionReference(t, &api.Reference{
		SchemaVersion: api.SchemaVersion,
		ID:            "montferret/core",
		Version:       "1.0.0",
		Namespaces: []api.Namespace{
			{
				Name: "sample",
				Functions: []api.Function{
					{
						Name: "dothing",
						Signatures: []api.Signature{
							{
								Parameters:  []api.Parameter{{Name: "value", Type: "Any", Description: "Input value."}},
								Description: "Handles one value.",
								Return:      &api.Return{Type: "Any", Description: "Handled value."},
								Deprecated:  "Use sample::next instead.",
							},
							{
								Parameters:  []api.Parameter{{Name: "values", Type: "Any", Description: "Input values."}},
								Variadic:    true,
								Description: "Handles repeated values.",
								Return:      &api.Return{Type: "Any", Description: "Handled values."},
								Throws:      []api.Throw{{Error: "TypeError", Description: "A value is unsupported."}},
								Deprecated:  "Use sample::next instead.",
							},
							{
								Parameters: []api.Parameter{
									{Name: "one"}, {Name: "two"}, {Name: "three"}, {Name: "four"},
								},
							},
						},
					},
					{
						Name: "old",
						Signatures: []api.Signature{{
							Parameters: []api.Parameter{{Name: "value"}},
							Deprecated: "Use sample::next instead.",
						}},
					},
					{Name: "reference_only", Signatures: []api.Signature{{Parameters: []api.Parameter{}}}},
				},
			},
		},
	})

	index := newFunctionIndex(functions, metadataFromStdlibReference(reference))
	function, ok := index.lookup("SAMPLE::DOTHING")
	if !ok {
		t.Fatal("enriched function lookup failed")
	}

	if function.name != "sample::dothing" || function.namespace != "sample" || len(function.signatures) != 3 {
		t.Fatalf("enriched function = %+v", function)
	}

	if signature := function.signatures[0]; signature.Label != "sample::dothing(value)" ||
		signature.Description != "Handles one value." || signature.Return == nil || signature.Return.Type != "Any" {
		t.Fatalf("fixed enriched signature = %+v", signature)
	}

	if signature := function.signatures[1]; signature.Label != "sample::dothing(arg1, arg2)" || signature.Description != "" {
		t.Fatalf("runtime-only arity fallback = %+v", signature)
	}

	if signature := function.signatures[2]; signature.Label != "sample::dothing(values...)" || !signature.Variadic ||
		len(signature.Throws) != 1 || signature.Throws[0].Error != "TypeError" {
		t.Fatalf("variadic enriched signature = %+v", signature)
	}

	if function.deprecated {
		t.Fatal("partially deprecated function marked wholly deprecated")
	}

	old, ok := index.lookup("sample::old")
	if !ok || !old.deprecated {
		t.Fatalf("wholly deprecated function = %+v, found %t", old, ok)
	}

	if _, ok := index.lookup("sample::reference_only"); ok {
		t.Fatal("reference-only function escaped into runtime catalog")
	}
}

func TestAllSignaturesDeprecatedRequiresEveryExecutableOverload(t *testing.T) {
	tests := []struct {
		name       string
		signatures []Signature
		want       bool
	}{
		{name: "none"},
		{name: "all", signatures: []Signature{{Deprecated: "Use next."}, {Deprecated: "Use next."}}, want: true},
		{name: "partial", signatures: []Signature{{Deprecated: "Use next."}, {}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := allSignaturesDeprecated(test.signatures); got != test.want {
				t.Fatalf("allSignaturesDeprecated() = %t, want %t", got, test.want)
			}
		})
	}
}

func parseFunctionReference(t *testing.T, reference *api.Reference) *stdlibref.Reference {
	t.Helper()

	data, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := stdlibref.Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	return parsed
}
