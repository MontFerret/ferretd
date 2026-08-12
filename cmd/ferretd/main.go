package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/MontFerret/ferretd/internal/daemon"
	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/lsp"
	"github.com/MontFerret/ferretd/internal/transport"
)

var version = "dev"
var runLSP = serveLSP

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := execute(ctx, newRootCommand(version), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ferretd: %v\n", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, root *cobra.Command, args []string) error {
	if len(args) > 1 && (args[0] == "--version" || args[0] == "-v") {
		return errors.New("--version does not accept arguments")
	}

	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

func newRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "ferretd",
		Short:         "Ferret developer service",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("missing command; run ferretd --help for usage")
		},
	}

	root.SetVersionTemplate("ferretd {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newServeCommand(version),
		&cobra.Command{
			Use:   "lsp",
			Short: "Start the language server over stdio",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runLSP(cmd.Context())
			},
		},
	)

	return root
}

func newServeCommand(version string) *cobra.Command {
	var endpointValue string

	command := &cobra.Command{
		Use:   "serve",
		Short: "Start the local daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serve(cmd.Context(), version, endpointValue, cmd.ErrOrStderr())
		},
	}
	command.Flags().StringVar(&endpointValue, "endpoint", "", "local endpoint URL")

	return command
}

func serveLSP(ctx context.Context) error {
	server := lsp.New(language.New())
	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("run LSP server: %w", err)
	}
	return nil
}

func serve(ctx context.Context, version, endpointValue string, stderr io.Writer) error {
	var endpoint transport.Endpoint
	if endpointValue != "" {
		var err error
		endpoint, err = transport.ParseEndpoint(endpointValue)
		if err != nil {
			return fmt.Errorf("parse daemon endpoint: %w", err)
		}
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
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
