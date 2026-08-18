package main

import (
	"context"
	"errors"
	"testing"
)

func TestDAPCommandDelegatesContextAndRejectsArguments(t *testing.T) {
	original := runDAP
	t.Cleanup(func() { runDAP = original })

	called := false
	runDAP = func(ctx context.Context) error {
		called = true
		if ctx == nil {
			t.Fatal("DAP context is nil")
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
	runDAP = func(context.Context) error { return want }
	if err := execute(context.Background(), newRootCommand("test"), []string{"dap"}); !errors.Is(err, want) {
		t.Fatalf("execute dap error = %v, want %v", err, want)
	}
}
