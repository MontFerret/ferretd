package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/MontFerret/ferretd/internal/daemon"
	"github.com/MontFerret/ferretd/internal/transport"
)

func newServeCommand(version string) *cobra.Command {
	var endpointValue string
	var logLevelValue string

	command := &cobra.Command{
		Use:   "serve",
		Short: "Start the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cmd.Context(), version, endpointValue, logLevelValue, cmd.ErrOrStderr())
		},
	}
	command.Flags().StringVar(&endpointValue, "endpoint", "", "local endpoint URL")
	command.Flags().StringVar(&logLevelValue, "log-level", defaultLogLevel, "log level (debug, info, warn, error)")

	return command
}

func serve(ctx context.Context, version, endpointValue, logLevelValue string, stderr io.Writer) error {
	var endpoint transport.Endpoint
	if endpointValue != "" {
		var err error
		endpoint, err = transport.ParseEndpoint(endpointValue)
		if err != nil {
			return fmt.Errorf("parse daemon endpoint: %w", err)
		}
	}

	logger, err := newLogger(stderr, logLevelValue)
	if err != nil {
		return err
	}

	d, err := daemon.New(daemon.Options{
		Version:  version,
		Endpoint: endpoint,
		Logger:   logger,
	})
	if err != nil {
		return fmt.Errorf("create daemon: %w", err)
	}

	startErr := d.Start(ctx)

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stopErr := d.Stop(stopCtx)
	if startErr != nil {
		return fmt.Errorf("start daemon: %w", startErr)
	}

	if stopErr != nil {
		return fmt.Errorf("stop daemon: %w", stopErr)
	}

	return nil
}
