package main

import (
	"context"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

func loadRuntimeConfig(ctx context.Context, path string, supplied bool) (runtimes.Config, error) {
	if !supplied {
		return runtimes.Config{}, nil
	}

	return runtimes.Load(ctx, path)
}
