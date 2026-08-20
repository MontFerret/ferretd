package debug

import "testing"

func TestSessionOptionsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		input SessionOptions
		want  string
	}{
		{name: "zero", want: defaultOutputContentType},
		{name: "whitespace", input: SessionOptions{OutputContentType: " \t\n"}, want: defaultOutputContentType},
		{name: "trimmed", input: SessionOptions{OutputContentType: " text/plain "}, want: "text/plain"},
		{name: "explicit", input: SessionOptions{OutputContentType: "application/msgpack"}, want: "application/msgpack"},
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
