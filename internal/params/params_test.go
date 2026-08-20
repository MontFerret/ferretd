package params

import (
	"reflect"
	"testing"
)

func TestCloneRecursivelyCopiesBoundaryContainers(t *testing.T) {
	input := map[string]any{
		"nil":    nil,
		"string": "value",
		"bool":   true,
		"int":    42,
		"float":  4.5,
		"map":    map[string]any{"items": []any{"one", map[string]any{"key": "value"}}},
		"slice":  []any{map[string]any{"key": "value"}, []any{"nested"}},
	}

	cloned := Clone(input)
	input["map"].(map[string]any)["items"].([]any)[0] = "caller"
	input["slice"].([]any)[0].(map[string]any)["key"] = "caller"
	cloned["map"].(map[string]any)["items"].([]any)[1].(map[string]any)["key"] = "snapshot"
	cloned["slice"].([]any)[1].([]any)[0] = "snapshot"

	if got := cloned["map"].(map[string]any)["items"].([]any)[0]; got != "one" {
		t.Fatalf("cloned map item = %v, want one", got)
	}
	if got := cloned["slice"].([]any)[0].(map[string]any)["key"]; got != "value" {
		t.Fatalf("cloned slice map = %v, want value", got)
	}
	if got := input["map"].(map[string]any)["items"].([]any)[1].(map[string]any)["key"]; got != "value" {
		t.Fatalf("input nested map = %v, want value", got)
	}
	if got := input["slice"].([]any)[1].([]any)[0]; got != "nested" {
		t.Fatalf("input nested slice = %v, want nested", got)
	}
}

func TestClonePreservesNil(t *testing.T) {
	if Clone(nil) != nil {
		t.Fatal("Clone(nil) returned a non-nil map")
	}
}

func TestPrepareConvertsOwnedCopy(t *testing.T) {
	input := map[string]any{
		"value":  7,
		"nested": map[string]any{"items": []any{"one", "two"}},
	}

	converted, retained, err := Prepare(input)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	input["value"] = 99
	input["nested"].(map[string]any)["items"].([]any)[0] = "changed"

	if got := retained["value"]; got != 7 {
		t.Fatalf("retained value = %v, want 7", got)
	}
	if got := retained["nested"].(map[string]any)["items"].([]any)[0]; got != "one" {
		t.Fatalf("retained nested item = %v, want one", got)
	}
	if _, ok := converted.Get("nested"); !ok {
		t.Fatal("converted parameters do not contain nested")
	}
}

func TestPreparePreservesFerretAcceptedValues(t *testing.T) {
	values := map[string]any{"typedSlice": []string{"one", "two"}}

	_, retained, err := Prepare(values)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !reflect.DeepEqual(retained["typedSlice"], []string{"one", "two"}) {
		t.Fatalf("retained typed slice = %#v", retained["typedSlice"])
	}
}

func TestPrepareRejectsFerretInvalidValues(t *testing.T) {
	if _, _, err := Prepare(map[string]any{"invalid": make(chan int)}); err == nil {
		t.Fatal("Prepare accepted a channel value")
	}
}
