package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MontFerret/ferretd/internal/daemon"
	"github.com/MontFerret/ferretd/internal/transport"
)

func newServeCommand(version string) *cobra.Command {
	var endpointValue string
	var bearerTokenEnvironment string
	var logLevelValue string

	command := &cobra.Command{
		Use:   "serve",
		Short: "Start the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(
				cmd.Context(),
				version,
				endpointValue,
				bearerTokenEnvironment,
				logLevel(logLevelValue),
				cmd.ErrOrStderr(),
			)
		},
	}
	command.Flags().StringVar(&endpointValue, "endpoint", "", "local endpoint URL")
	command.Flags().StringVar(
		&bearerTokenEnvironment,
		"auth-token-env",
		"",
		"environment variable containing the TCP bearer token",
	)
	command.Flags().StringVar(
		&logLevelValue,
		"log-level",
		defaultLogLevel.String(),
		"log level (debug, info, warn, error)",
	)

	return command
}

func serve(
	ctx context.Context,
	version string,
	endpointValue string,
	bearerTokenEnvironment string,
	logLevelValue logLevel,
	stderr io.Writer,
) error {
	var endpoint transport.Endpoint

	if endpointValue != "" {
		var err error

		endpoint, err = transport.ParseEndpoint(endpointValue)
		if err != nil {
			return fmt.Errorf("parse daemon endpoint: %w", err)
		}
	}

	bearerToken, err := resolveBearerToken(endpoint, bearerTokenEnvironment)
	if err != nil {
		return err
	}

	logger, err := newLogger(stderr, logLevelValue)
	if err != nil {
		return err
	}

	d, err := daemon.New(daemon.Options{
		Version:     version,
		Endpoint:    endpoint,
		BearerToken: bearerToken,
		Logger:      logger,
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

func resolveBearerToken(endpoint transport.Endpoint, environment string) (string, error) {
	if endpoint.Network != transport.NetworkTCP {
		if environment != "" {
			return "", fmt.Errorf("--auth-token-env is only supported with a TCP endpoint")
		}

		return "", nil
	}

	if environment == "" {
		return "", fmt.Errorf("TCP endpoint requires --auth-token-env")
	}

	if strings.TrimSpace(environment) != environment {
		return "", fmt.Errorf("--auth-token-env must name an environment variable without surrounding whitespace")
	}

	token, ok := os.LookupEnv(environment)
	if !ok || token == "" {
		return "", fmt.Errorf("TCP bearer token environment variable %s is missing or empty", environment)
	}

	return token, nil
}
