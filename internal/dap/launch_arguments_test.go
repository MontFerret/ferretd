package dap

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/MontFerret/ferretd/internal/exec"
)

func TestLaunchArgumentsRuntimeOptions(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		want      string
		invalid   bool
	}{
		{name: "omitted", arguments: `{}`},
		{name: "null", arguments: `{"workingDirectory":null}`, invalid: true},
		{name: "empty", arguments: `{"workingDirectory":""}`, invalid: true},
		{name: "number", arguments: `{"workingDirectory":42}`, invalid: true},
		{name: "boolean", arguments: `{"workingDirectory":true}`, invalid: true},
		{name: "object", arguments: `{"workingDirectory":{"path":"/runtime"}}`, invalid: true},
		{name: "array", arguments: `{"workingDirectory":["/runtime"]}`, invalid: true},
		{name: "whitespace deferred", arguments: `{"workingDirectory":" \t "}`, want: " \t "},
		{name: "relative deferred", arguments: `{"workingDirectory":"runtime"}`, want: "runtime"},
		{name: "absolute", arguments: `{"workingDirectory":"/runtime"}`, want: "/runtime"},
		{name: "normalization deferred", arguments: `{"workingDirectory":" /runtime/./ "}`, want: " /runtime/./ "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var arguments launchArguments
			if err := json.Unmarshal([]byte(test.arguments), &arguments); err != nil {
				t.Fatal(err)
			}

			options, err := arguments.runtimeOptions()

			if test.invalid {
				if !errors.Is(err, exec.ErrInvalidExecutionOptions) {
					t.Fatalf("runtimeOptions error = %v, want ErrInvalidExecutionOptions", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("runtimeOptions: %v", err)
			}

			if options.WorkingDirectory != test.want {
				t.Fatalf("working directory = %q, want %q", options.WorkingDirectory, test.want)
			}
		})
	}
}
