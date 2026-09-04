package exec

import (
	"reflect"
	"testing"
)

func TestParametersCloneRecursivelyCopiesBoundaryContainers(t *testing.T) {
	input := Parameters{
		"nil":    nil,
		"string": "value",
		"bool":   true,
		"int":    42,
		"float":  4.5,
		"map":    map[string]any{"items": []any{"one", map[string]any{"key": "value"}}},
		"slice":  []any{map[string]any{"key": "value"}, []any{"nested"}},
	}

	cloned := input.Clone()
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

func TestParametersClonePreservesNil(t *testing.T) {
	if Parameters(nil).Clone() != nil {
		t.Fatal("Parameters(nil).Clone() returned a non-nil map")
	}
}

func TestParametersCloneReturnsOwnedCopy(t *testing.T) {
	input := Parameters{
		"value":  7,
		"nested": map[string]any{"items": []any{"one", "two"}},
	}

	retained := input.Clone()

	input["value"] = 99
	input["nested"].(map[string]any)["items"].([]any)[0] = "changed"

	if got := retained["value"]; got != 7 {
		t.Fatalf("retained value = %v, want 7", got)
	}
	if got := retained["nested"].(map[string]any)["items"].([]any)[0]; got != "one" {
		t.Fatalf("retained nested item = %v, want one", got)
	}
}

func TestParametersClonePreservesTypedValues(t *testing.T) {
	values := Parameters{"typedSlice": []string{"one", "two"}}

	retained := values.Clone()
	if !reflect.DeepEqual(retained["typedSlice"], []string{"one", "two"}) {
		t.Fatalf("retained typed slice = %#v", retained["typedSlice"])
	}
}
