package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	for _, defaultRuntime := range []string{"local", "app"} {
		t.Run(defaultRuntime, func(t *testing.T) { testServeRuntimeDiscovery(t, defaultRuntime) })
	}
}

func testServeRuntimeDiscovery(t *testing.T, defaultRuntime string) {
	t.Helper()
	path := writeRuntimeConfig(t, fmt.Sprintf(`{"defaultRuntime":%q,"runtimes":[{"name":"app","label":"Application","provider":"wire","endpoint":"tcp://application.invalid:9000"}]}`, defaultRuntime))
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
	if err != nil || snapshot.DefaultRuntime != defaultRuntime || len(snapshot.Runtimes) != 2 || snapshot.Runtimes[0].Name != "app" || snapshot.Runtimes[0].Label != "Application" {
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

func TestDAPProcessWireDefaultInitializesBeforeAcquisition(t *testing.T) {
	binary := buildDAPProcess(t)
	path := writeRuntimeConfig(t, `{"defaultRuntime":"application","runtimes":[{"name":"application","provider":"wire","endpoint":"tcp://127.0.0.1:1","insecure":true,"connectTimeout":"20ms"}]}`)
	client := newDAPProcessClient(t, binary, "--runtime-config", path)
	client.send("initialize", map[string]any{})
	initialized := client.response("initialize")

	var capabilities map[string]bool
	if err := json.Unmarshal(initialized.Body, &capabilities); err != nil || !capabilities["supportsFerretRuntimeSelection"] || !capabilities["supportsCancelRequest"] {
		t.Fatalf("initialize body = %s, %v", initialized.Body, err)
	}

	program := filepath.Join(t.TempDir(), "query.fql")
	if err := os.WriteFile(program, []byte("RETURN 1"), 0o600); err != nil {
		t.Fatal(err)
	}

	client.send("launch", map[string]any{"program": program})

	failure := client.read()
	if failure.Type != "response" || failure.Command != "launch" || failure.Success || !strings.Contains(string(failure.Body), "application") {
		t.Fatalf("unavailable default response = %+v", failure)
	}

	client.send("launch", map[string]any{"program": program, "runtimeName": "local", "stopOnEntry": true})
	client.event("initialized")
	client.send("configurationDone", map[string]any{})
	client.response("configurationDone")

	acknowledgment := client.response("launch")
	if !strings.Contains(string(acknowledgment.Body), `"runtimeName":"local"`) {
		t.Fatalf("launch body = %s", acknowledgment.Body)
	}

	client.event("stopped")
	client.disconnect()

	if !strings.Contains(client.stderr.String(), `"runtime_name":"local"`) {
		t.Fatal("missing runtime correlation on stderr")
	}
}
