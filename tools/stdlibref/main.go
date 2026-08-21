package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

const defaultOutput = "./internal/language/stdlib/api.json"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "stdlibref: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing mode: expected sync or check")
	}

	flags := flag.NewFlagSet("stdlibref "+args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	output := flags.String("output", defaultOutput, "embedded Standard Library API Reference path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	command, err := newCommand(*output, 30*time.Second)
	if err != nil {
		return err
	}

	return command.run(ctx, args[0])
}
