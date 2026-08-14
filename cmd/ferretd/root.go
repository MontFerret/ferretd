package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

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
		newLSPCommand(),
	)

	return root
}
