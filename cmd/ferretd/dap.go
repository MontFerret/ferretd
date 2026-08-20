package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	ferretdap "github.com/MontFerret/ferretd/internal/dap"
)

var runDAP = serveDAP

func newDAPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dap",
		Short: "Start the debug adapter over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDAP(cmd.Context())
		},
	}
}

func serveDAP(ctx context.Context) error {
	server, err := ferretdap.New(os.Stdin, os.Stdout)
	if err != nil {
		return fmt.Errorf("create DAP server: %w", err)
	}

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run DAP server: %w", err)
	}

	return nil
}
