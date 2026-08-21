package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/lsp"
	"github.com/MontFerret/ferretd/internal/workspace"
)

var runLSP = serveLSP

func newLSPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lsp",
		Short: "Start the language server over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLSP(cmd.Context(), cmd.ErrOrStderr())
		},
	}
}

func serveLSP(ctx context.Context, stderr io.Writer) error {
	workspaces := workspace.New()
	functions, warnings, err := language.NewDefaultFunctionCatalog()
	if err != nil {
		return fmt.Errorf("create default function catalog: %w", err)
	}

	if err := reportCatalogWarnings(stderr, warnings); err != nil {
		return err
	}

	service, err := language.New(workspaces, functions, language.Options{})
	if err != nil {
		return fmt.Errorf("create language service: %w", err)
	}

	server, err := lsp.New(service)
	if err != nil {
		return fmt.Errorf("create LSP server: %w", err)
	}

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run LSP server: %w", err)
	}

	return nil
}

func reportCatalogWarnings(stderr io.Writer, warnings []language.CatalogWarning) error {
	referenceOnly := make([]string, 0)
	runtimeOnly := make([]string, 0)

	for _, warning := range warnings {
		switch warning.Kind {
		case language.CatalogWarningReferenceOnly:
			referenceOnly = append(referenceOnly, warning.Name)
		case language.CatalogWarningRuntimeOnly:
			runtimeOnly = append(runtimeOnly, warning.Name)
		}
	}

	if len(referenceOnly) > 0 {
		if _, err := fmt.Fprintf(stderr, "ferretd: warning: Standard Library API Reference functions are unavailable at runtime and were omitted: %s\n", strings.Join(referenceOnly, ", ")); err != nil {
			return fmt.Errorf("report Standard Library API Reference mismatch: %w", err)
		}
	}

	if len(runtimeOnly) > 0 {
		if _, err := fmt.Fprintf(stderr, "ferretd: warning: Standard Library runtime functions lack API metadata and use fallback signatures: %s\n", strings.Join(runtimeOnly, ", ")); err != nil {
			return fmt.Errorf("report Standard Library runtime mismatch: %w", err)
		}
	}

	return nil
}
