package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

const defaultOutput = "internal/language/stdlib/api.json"

func main() {
	if err := runMain(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMain(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("stdlibref", flag.ContinueOnError)
	check := flags.Bool("check", false, "verify the checked-in reference without downloading")
	output := flags.String("output", defaultOutput, "embedded API Reference output path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return run(ctx, options{root: root, output: *output, check: *check, client: client, indexURL: indexURL})
}
