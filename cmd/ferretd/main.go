// Package main runs the ferretd developer service and its protocol adapters.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := execute(ctx, newRootCommand(version), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ferretd: %v\n", err)
		os.Exit(1)
	}
}
