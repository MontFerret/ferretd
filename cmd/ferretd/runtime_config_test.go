package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

func TestDAPCommandPassesRuntimeConfiguration(t *testing.T) {
	path := writeRuntimeConfig(t, `{"runtimes":[{"name":"app","provider":"wire","endpoint":"tcp://application.invalid:9000","connectTimeout":"2s"}]}`)
	original := runDAP
	t.Cleanup(func() { runDAP = original })
	called := false
	runDAP = func(_ context.Context, _ *zerolog.Logger, config runtimes.Config) error {
		called = true

		catalog, err := runtimes.NewCatalog(config)
		if err != nil {
			t.Fatal(err)
		}

		definition, err := catalog.Resolve("app")
		if err != nil || definition.Endpoint != "tcp://application.invalid:9000" || definition.ConnectTimeout != 2*time.Second || catalog.DefaultName() != "local" {
			t.Fatalf("config = %+v, %v", definition, err)
		}

		return nil
	}

	if err := execute(t.Context(), newRootCommand("test"), []string{"dap", "--runtime-config", path}); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("DAP runner not called")
	}
}

func TestServeCommandRuntimeDiscovery(t *testing.T) {
	path := writeRuntimeConfig(t, `{"defaultRuntime":"local","runtimes":[{"name":"app","label":"Application","provider":"wire","endpoint":"tcp://application.invalid:9000"}]}`)
	endpoint := testClientEndpoint(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := executeCommand(ctx, "test", "serve", "--endpoint", endpoint.String(), "--runtime-config", path)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()

		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()

		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-cleanupContext.Done():
			t.Error("serve did not stop during cleanup")
		}
	})
	c := waitForClient(t, endpoint)
	t.Cleanup(func() { _ = c.Close() })

	// Startup retained the parsed snapshot; later file changes are not reloaded.
	if err := os.WriteFile(path, []byte("invalid after startup"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := c.ListRuntimes(t.Context())
	if err != nil || snapshot.DefaultRuntime != "local" || len(snapshot.Runtimes) != 2 || snapshot.Runtimes[0].Name != "app" || snapshot.Runtimes[0].Label != "Application" {
		t.Fatalf("discovery = %+v, %v", snapshot, err)
	}
}

func TestRuntimeConfigurationProcessErrorsUseStderr(t *testing.T) {
	binary := buildDAPProcess(t)
	for _, command := range []string{"dap", "serve"} {
		for _, test := range []struct {
			name string
			path string
			want string
		}{
			{"missing", filepath.Join(t.TempDir(), "missing"), "read runtime configuration"},
			{"empty path", "", "must not be blank"},
			{"malformed", writeRuntimeConfig(t, "{"), "invalid runtime configuration"},
			{"null", writeRuntimeConfig(t, "null"), "invalid runtime configuration"},
			{"unknown field", writeRuntimeConfig(t, `{"unexpected":true}`), "invalid runtime configuration"},
			{"unknown default", writeRuntimeConfig(t, `{"defaultRuntime":"missing"}`), "runtime not found"},
			{"Wire default", writeRuntimeConfig(t, `{"defaultRuntime":"app","runtimes":[{"name":"app","provider":"wire","endpoint":"tcp://application.invalid:9000"}]}`), "unsupported default runtime"},
		} {
			t.Run(command+"/"+test.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				process := exec.CommandContext(ctx, binary, command, "--runtime-config", test.path)
				var stdout, stderr bytes.Buffer
				process.Stdout = &stdout
				process.Stderr = &stderr

				if err := process.Run(); err == nil {
					t.Fatal("invalid configuration exited successfully")
				}

				if ctx.Err() != nil {
					t.Fatal("invalid configuration did not fail promptly")
				}

				if stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
					t.Fatalf("stdout = %q; stderr = %q", stdout.String(), stderr.String())
				}
			})
		}
	}
}

func writeRuntimeConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "runtimes.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
