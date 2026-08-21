package language

import (
	"testing"

	"github.com/MontFerret/specs/pkg/api"
)

func TestRenderAPIType(t *testing.T) {
	named := func(name string) api.Type {
		return api.Type{Kind: api.TypeKindNamed, Name: name}
	}
	union := func(values ...api.Type) api.Type {
		return api.Type{Kind: api.TypeKindUnion, Types: values}
	}
	list := func(value api.Type) api.Type {
		return api.Type{Kind: api.TypeKindList, Element: &value}
	}

	tests := []struct {
		name  string
		value *api.Type
		want  string
	}{
		{name: "missing", want: ""},
		{name: "named", value: apiType(named("String")), want: "String"},
		{name: "union", value: apiType(union(named("String"), named("Array"), named("Object"))), want: "String | Array | Object"},
		{name: "list", value: apiType(list(named("Int"))), want: "[Int]"},
		{name: "list union", value: apiType(list(union(named("Int"), named("Float")))), want: "[Int | Float]"},
		{name: "nested list", value: apiType(list(list(named("String")))), want: "[[String]]"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := renderAPIType(test.value); got != test.want {
				t.Fatalf("renderAPIType() = %q, want %q", got, test.want)
			}
		})
	}
}

func apiType(value api.Type) *api.Type {
	return &value
}
