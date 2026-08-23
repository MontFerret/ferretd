package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	ferretdap "github.com/MontFerret/ferretd/internal/dap"
)

var runDAP = serveDAP

func newDAPCommand() *cobra.Command {
	var logLevelValue string

	command := &cobra.Command{
		Use:   "dap",
		Short: "Start the debug adapter over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := newLogger(cmd.ErrOrStderr(), logLevel(logLevelValue))
			if err != nil {
				return err
			}

			return runDAP(cmd.Context(), logger)
		},
	}
	command.Flags().StringVar(
		&logLevelValue,
		"log-level",
		defaultLogLevel.String(),
		"log level (debug, info, warn, error)",
	)

	return command
}

func serveDAP(ctx context.Context, logger *zerolog.Logger) error {
	server, err := ferretdap.New(os.Stdin, os.Stdout, ferretdap.Options{Logger: logger})
	if err != nil {
		return fmt.Errorf("create DAP server: %w", err)
	}

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run DAP server: %w", err)
	}

	return nil
}
