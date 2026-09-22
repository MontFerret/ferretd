package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func buildDAPProcess(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "ferretd")

	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	// Build the real command with this repository's released Ferret dependency.
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOWORK=off")

	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ferretd: %v\n%s", err, output)
	}

	return binary
}
