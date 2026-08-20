package exec

import "testing"

func TestExecutionOptionsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		input ExecutionOptions
		want  string
	}{
		{name: "zero", want: defaultOutputContentType},
		{name: "whitespace", input: ExecutionOptions{OutputContentType: " \t\n"}, want: defaultOutputContentType},
		{name: "trimmed", input: ExecutionOptions{OutputContentType: " text/plain "}, want: "text/plain"},
		{name: "explicit", input: ExecutionOptions{OutputContentType: "application/msgpack"}, want: "application/msgpack"},
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
