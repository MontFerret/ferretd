package language

import (
	"context"
	"reflect"
	"testing"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/specs/pkg/api"
)

func TestFunctionCatalogMergesAuthoritativeReferenceAndPreservesFallback(t *testing.T) {
	library := runtime.NewLibrary()
	library.Namespace("CuStOm").Function().A1().Add("DoThing", func(context.Context, runtime.Value) (runtime.Value, error) {
		return runtime.None, nil
	})
	library.Namespace("Host").Function().Var().Add("Only", func(context.Context, ...runtime.Value) (runtime.Value, error) {
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

	reference := testCatalogReference()
	warnings := catalog.mergeReference(reference)

	wantWarnings := []CatalogWarning{
		{Kind: CatalogWarningReferenceOnly, Name: "custom::api_only"},
		{Kind: CatalogWarningRuntimeOnly, Name: "Host::Only"},
	}
	if !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("warnings = %+v, want %+v", warnings, wantWarnings)
	}

	function, ok := catalog.lookup("CUSTOM::DOTHING")
	if !ok {
		t.Fatal("case-insensitive lookup failed")
	}

	if function.name != "CuStOm::DoThing" || !function.authored || len(function.signatures) != 2 {
		t.Fatalf("merged function = %+v", function)
	}

	if got := function.signatures[0].Label; got != "CuStOm::DoThing(value: String | Array)" {
		t.Fatalf("first signature label = %q", got)
	}

	if got := function.signatures[1].Label; got != "CuStOm::DoThing(value: String, options: [Int | Float])" {
		t.Fatalf("second signature label = %q", got)
	}

	if function.signatures[0].Return == nil || function.signatures[0].Return.Type != "Object" ||
		function.signatures[0].Parameters[0].Description != "Input value." ||
		function.signatures[0].Throws[0].Error != "TypeError" ||
		function.signatures[0].Deprecated != "Use custom::replacement." {
		t.Fatalf("authored metadata = %+v", function.signatures[0])
	}

	// Runtime registration exposes only arity one, but both authored API
	// signatures remain intact and ordered.
	if got := function.renderedSignatures(7); !reflect.DeepEqual(got, function.signatures) {
		t.Fatalf("rendered signatures = %+v, want %+v", got, function.signatures)
	}

	rendered := function.renderedSignatures(7)
	rendered[0].Parameters[0].Name = "mutated"

	rendered[0].Return.Type = "Mutated"
	if function.signatures[0].Parameters[0].Name != "value" || function.signatures[0].Return.Type != "Object" {
		t.Fatal("catalog exposed mutable signature metadata")
	}

	reference.Namespaces[0].Functions[0].Signatures[0].Parameters[0].Name = "mutated"
	if function.signatures[0].Parameters[0].Name != "value" {
		t.Fatal("catalog retained mutable API Reference data")
	}

	if _, ok := catalog.lookup("custom::api_only"); ok {
		t.Fatal("API-only function entered executable catalog")
	}

	host, ok := catalog.lookup("host::only")
	if !ok {
		t.Fatal("runtime-only host function is missing")
	}

	if host.detail != "registered function" || host.documentation != "" {
		t.Fatalf("runtime-only completion metadata = %q, %q", host.detail, host.documentation)
	}

	wantFallback := []Signature{{
		Label:      "Host::Only(arg1, arg2, arg3...)",
		Parameters: []SignatureParameter{{Name: "arg1", Label: "arg1"}, {Name: "arg2", Label: "arg2"}, {Name: "arg3", Label: "arg3...", Variadic: true}},
		Variadic:   true,
	}}
	if got := host.renderedSignatures(3); !reflect.DeepEqual(got, wantFallback) {
		t.Fatalf("runtime fallback = %+v, want %+v", got, wantFallback)
	}
}

func TestFunctionCatalogDeprecatesCompletionOnlyWhenEverySignatureIsDeprecated(t *testing.T) {
	function := functionSymbol{authored: true, signatures: []Signature{{Deprecated: "old"}, {}}}
	function.cacheCompletion()

	if function.deprecated {
		t.Fatal("mixed overload deprecation marked the function deprecated")
	}

	function.signatures[1].Deprecated = "also old"
	function.cacheCompletion()

	if !function.deprecated {
		t.Fatal("fully deprecated function was not marked deprecated")
	}

	runtimeOnly := functionSymbol{runtimeVariadic: true}
	runtimeOnly.cacheCompletion()

	if runtimeOnly.deprecated {
		t.Fatal("runtime fallback was marked deprecated")
	}
}

func TestStructuredReferenceRejectsLegacyStringTypes(t *testing.T) {
	structured := []byte(`{"schemaVersion":1,"id":"acme/module","version":"1.0.0","namespaces":[{"name":"custom","functions":[{"name":"read","signatures":[{"parameters":[{"name":"value","type":{"kind":"list","element":{"kind":"union","types":[{"kind":"named","name":"Int"},{"kind":"named","name":"Float"}]}},"description":"Input."}]}]}]}]}`)

	reference, err := api.Parse(structured)
	if err != nil {
		t.Fatalf("parse structured reference: %v", err)
	}

	functions := newReferenceFunctions(reference)
	if got := functions[0].signatures[0].Parameters[0].Type; got != "[Int | Float]" {
		t.Fatalf("structured parameter type = %q", got)
	}

	legacy := []byte(`{"schemaVersion":1,"id":"acme/module","version":"1.0.0","namespaces":[{"name":"custom","functions":[{"name":"read","signatures":[{"parameters":[{"name":"value","type":"[Int | Float]","description":"Input."}]}]}]}]}`)
	if _, err := api.Parse(legacy); err == nil {
		t.Fatal("legacy string type parsed successfully")
	}
}

func testCatalogReference() *api.Reference {
	stringType := api.Type{Kind: api.TypeKindNamed, Name: "String"}
	arrayType := api.Type{Kind: api.TypeKindNamed, Name: "Array"}
	intType := api.Type{Kind: api.TypeKindNamed, Name: "Int"}
	floatType := api.Type{Kind: api.TypeKindNamed, Name: "Float"}
	objectType := api.Type{Kind: api.TypeKindNamed, Name: "Object"}

	return &api.Reference{
		SchemaVersion: api.SchemaVersion,
		ID:            "acme/module",
		Version:       "1.0.0",
		Namespaces: []api.Namespace{{
			Name: "custom",
			Functions: []api.Function{
				{
					Name: "dothing",
					Signatures: []api.Signature{
						{
							Parameters: []api.Parameter{{
								Name:        "value",
								Type:        &api.Type{Kind: api.TypeKindUnion, Types: []api.Type{stringType, arrayType}},
								Description: "Input value.",
							}},
							Description: "Does the thing.",
							Return:      &api.Return{Type: &objectType, Description: "Result value."},
							Throws:      []api.Throw{{Error: "TypeError", Description: "Input is invalid."}},
							Deprecated:  "Use custom::replacement.",
						},
						{
							Parameters: []api.Parameter{
								{Name: "value", Type: &stringType, Description: "Input value."},
								{Name: "options", Type: &api.Type{Kind: api.TypeKindList, Element: &api.Type{Kind: api.TypeKindUnion, Types: []api.Type{intType, floatType}}}, Description: "Options."},
							},
						},
					},
				},
				{Name: "api_only", Signatures: []api.Signature{{Parameters: []api.Parameter{}}}},
			},
		}},
	}
}
