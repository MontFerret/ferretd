package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/MontFerret/ferretd/internal/language"
)

func TestLSP(t *testing.T) {
	original := runLSP
	t.Cleanup(func() {
		runLSP = original
	})

	called := false
	runLSP = func(context.Context, io.Writer) error {
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

func TestReportCatalogWarningsUsesStderrOnly(t *testing.T) {
	var stderr bytes.Buffer
	warnings := []language.CatalogWarning{
		{Kind: language.CatalogWarningReferenceOnly, Name: "api::only"},
		{Kind: language.CatalogWarningRuntimeOnly, Name: "runtime::only"},
	}
	if err := reportCatalogWarnings(&stderr, warnings); err != nil {
		t.Fatal(err)
	}

	output := stderr.String()
	for _, want := range []string{"warning:", "api::only", "were omitted", "runtime::only", "fallback signatures"} {
		if !strings.Contains(output, want) {
			t.Fatalf("warning output = %q, want %q", output, want)
		}
	}
}
