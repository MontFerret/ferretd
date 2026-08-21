package exec

import "testing"

func TestRuntimeOptionsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		input RuntimeOptions
		want  string
	}{
		{name: "zero", want: defaultOutputContentType},
		{name: "whitespace", input: RuntimeOptions{OutputContentType: " \t\n"}, want: defaultOutputContentType},
		{name: "trimmed", input: RuntimeOptions{OutputContentType: " text/plain "}, want: "text/plain"},
		{name: "explicit", input: RuntimeOptions{OutputContentType: "application/msgpack"}, want: "application/msgpack"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.input.normalized()
			if got.OutputContentType != test.want {
				t.Fatalf("OutputContentType = %q, want %q", got.OutputContentType, test.want)
			}
		})
	}
}
