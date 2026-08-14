package main

import (
	"context"
	"testing"
)

func TestLSP(t *testing.T) {
	original := runLSP
	t.Cleanup(func() {
		runLSP = original
	})

	called := false
	runLSP = func(context.Context) error {
		called = true

		return nil
	}

	output, err := executeCommand(context.Background(), "test-version", "lsp")
	if err != nil {
		t.Fatalf("execute lsp: %v", err)
	}

	if output != "" {
		t.Fatalf("lsp output = %q, want protocol-pure stdout", output)
	}
	if !called {
		t.Fatal("lsp command did not run the LSP server")
	}
}
