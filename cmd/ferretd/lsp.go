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
	server := lsp.New(language.New(language.Options{Workspaces: workspace.New()}))
	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run LSP server: %w", err)
	}

	return nil
}
