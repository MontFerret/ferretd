package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	ferretdap "github.com/MontFerret/ferretd/internal/dap"
	"github.com/MontFerret/ferretd/internal/runtimes"
)

var runDAP = serveDAP

func newDAPCommand() *cobra.Command {
	var logLevelValue string
	var runtimeConfigPath string

	command := &cobra.Command{
		Use:   "dap",
		Short: "Start the debug adapter over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configuration, err := loadRuntimeConfig(cmd.Context(), runtimeConfigPath, cmd.Flags().Changed("runtime-config"))
			if err != nil {
				return err
			}

			logger, err := newLogger(cmd.ErrOrStderr(), logLevel(logLevelValue))
			if err != nil {
				return err
			}

			return runDAP(cmd.Context(), logger, configuration)
		},
	}
	command.Flags().StringVar(&runtimeConfigPath, "runtime-config", "", "runtime definitions JSON file")
	command.Flags().StringVar(
		&logLevelValue,
		"log-level",
		defaultLogLevel.String(),
		"log level (debug, info, warn, error)",
	)

	return command
}

func serveDAP(ctx context.Context, logger *zerolog.Logger, configuration runtimes.Config) error {
	server, err := ferretdap.New(os.Stdin, os.Stdout, ferretdap.Options{Logger: logger, RuntimeConfig: configuration})
	if err != nil {
		return fmt.Errorf("create DAP server: %w", err)
	}

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run DAP server: %w", err)
	}

	return nil
}
