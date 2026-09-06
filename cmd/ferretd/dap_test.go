package main

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

func TestDAPCommandDelegatesContextAndRejectsArguments(t *testing.T) {
	original := runDAP
	t.Cleanup(func() { runDAP = original })

	called := false
	runDAP = func(ctx context.Context, logger *zerolog.Logger) error {
		called = true

		if ctx == nil {
			t.Fatal("DAP context is nil")
		}

		if logger == nil {
			t.Fatal("DAP logger is nil")
		}

		if logger.GetLevel() != zerolog.InfoLevel {
			t.Fatal("DAP logger does not use the default info level")
		}

		return nil
	}

	if err := execute(context.Background(), newRootCommand("test"), []string{"dap"}); err != nil {
		t.Fatalf("execute dap: %v", err)
	}

	if !called {
		t.Fatal("DAP runner was not called")
	}

	if err := execute(context.Background(), newRootCommand("test"), []string{"dap", "extra"}); err == nil {
		t.Fatal("DAP command accepted positional arguments")
	}
}

func TestDAPCommandPreservesRunnerError(t *testing.T) {
	original := runDAP
	t.Cleanup(func() { runDAP = original })

	want := errors.New("DAP failed")
	runDAP = func(context.Context, *zerolog.Logger) error { return want }

	if err := execute(context.Background(), newRootCommand("test"), []string{"dap"}); !errors.Is(err, want) {
		t.Fatalf("execute dap error = %v, want %v", err, want)
	}
}

func TestDAPCommandPropagatesDebugLogLevel(t *testing.T) {
	original := runDAP
	t.Cleanup(func() { runDAP = original })

	runDAP = func(_ context.Context, logger *zerolog.Logger) error {
		if logger.GetLevel() != zerolog.DebugLevel {
			t.Fatal("DAP logger does not enable debug records")
		}

		return nil
	}

	if err := execute(context.Background(), newRootCommand("test"), []string{"dap", "--log-level", "debug"}); err != nil {
		t.Fatalf("execute dap: %v", err)
	}
}
