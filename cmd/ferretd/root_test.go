package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	output, err := executeCommand(context.Background(), "test-version", "--version")
	if err != nil {
		t.Fatalf("execute --version: %v", err)
	}

	if got, want := output, "ferretd test-version\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestHelp(t *testing.T) {
	output, err := executeCommand(context.Background(), "test-version", "--help")
	if err != nil {
		t.Fatalf("execute --help: %v", err)
	}

	for _, want := range []string{"Usage:", "serve", "lsp"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output = %q, want it to contain %q", output, want)
		}
	}
}

func TestRejectsInvalidCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing",
			want: "missing command",
		},
		{
			name: "unknown",
			args: []string{"unknown"},
			want: `unknown command "unknown" for "ferretd"`,
		},
		{
			name: "version arguments",
			args: []string{"--version", "unexpected"},
			want: "--version does not accept arguments",
		},
		{
			name: "serve arguments",
			args: []string{"serve", "unexpected"},
			want: "unknown command",
		},
		{
			name: "lsp arguments",
			args: []string{"lsp", "unexpected"},
			want: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeCommand(context.Background(), "test-version", tt.args...)
			if err == nil {
				t.Fatal("execute returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("execute error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestDefaultCompletionCommandIsDisabled(t *testing.T) {
	root := newRootCommand("test-version")
	if command, _, err := root.Find([]string{"completion"}); err == nil && command.Name() == "completion" {
		t.Fatal("default completion command is enabled")
	}
}

func executeCommand(ctx context.Context, version string, args ...string) (string, error) {
	var output bytes.Buffer
	root := newRootCommand(version)
	root.SetOut(&output)
	root.SetErr(&output)

	err := execute(ctx, root, args)

	return output.String(), err
}
