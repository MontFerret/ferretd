package main

import (
	"context"
	"fmt"

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
			return runLSP(cmd.Context())
		},
	}
}

func serveLSP(ctx context.Context) error {
	workspaces := workspace.New()
	functions, err := language.NewDefaultFunctions()
	if err != nil {
		return fmt.Errorf("create default language functions: %w", err)
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
