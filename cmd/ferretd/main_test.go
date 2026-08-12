package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/client"
)

func TestVersion(t *testing.T) {
	output, err := executeCommand(context.Background(), "test-version", "--version")
	if err != nil {
		t.Fatalf("execute --version: %v", err)
	}

	if got, want := output, "ferretd test-version\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestHelp(t *testing.T) {
	output, err := executeCommand(context.Background(), "test-version", "--help")
	if err != nil {
		t.Fatalf("execute --help: %v", err)
	}

	for _, want := range []string{"Usage:", "serve", "lsp"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output = %q, want it to contain %q", output, want)
		}
	}
}

func TestLSP(t *testing.T) {
	original := runLSP
	t.Cleanup(func() {
		runLSP = original
	})

	called := false
	runLSP = func(context.Context) error {
		called = true
		return nil
	}

	output, err := executeCommand(context.Background(), "test-version", "lsp")
	if err != nil {
		t.Fatalf("execute lsp: %v", err)
	}

	if output != "" {
		t.Fatalf("lsp output = %q, want protocol-pure stdout", output)
	}
	if !called {
		t.Fatal("lsp command did not run the LSP server")
	}
}

func TestRejectsInvalidCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing",
			want: "missing command",
		},
		{
			name: "unknown",
			args: []string{"unknown"},
			want: `unknown command "unknown" for "ferretd"`,
		},
		{
			name: "version arguments",
			args: []string{"--version", "unexpected"},
			want: "--version does not accept arguments",
		},
		{
			name: "serve arguments",
			args: []string{"serve", "unexpected"},
			want: "unknown command",
		},
		{
			name: "lsp arguments",
			args: []string{"lsp", "unexpected"},
			want: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executeCommand(context.Background(), "test-version", tt.args...)
			if err == nil {
				t.Fatal("execute returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("execute error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestServeStopsOnCancellation(t *testing.T) {
	endpoint := testClientEndpoint(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := executeCommand(ctx, "test-version", "serve", "--endpoint", endpoint.String())
		done <- err
	}()

	connection := waitForClient(t, endpoint)
	_ = connection.Close()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute serve: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}

	assertEndpointRemoved(t, endpoint)
}

func TestServeEndToEnd(t *testing.T) {
	endpoint := testClientEndpoint(t)
	done := make(chan error, 1)
	go func() {
		_, err := executeCommand(
			context.Background(),
			"test-version",
			"serve",
			"--endpoint",
			endpoint.String(),
		)
		done <- err
	}()

	connection := waitForClient(t, endpoint)
	info, err := connection.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Version != "test-version" || info.InstanceID == "" || info.APIVersion != (client.APIVersion{Major: 1}) {
		t.Fatalf("server info = %#v", info)
	}

	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	relativeRoot, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatalf("relative root: %v", err)
	}

	first, err := connection.Workspaces().Open(context.Background(), relativeRoot)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	second, err := connection.Workspaces().Open(context.Background(), root+string(os.PathSeparator))
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	if first != second {
		t.Fatalf("workspaces differ: %#v != %#v", first, second)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("Close client: %v", err)
	}

	connection = waitForClient(t, endpoint)
	items, err := connection.Workspaces().List(context.Background())
	if err != nil || len(items) != 1 || items[0] != first {
		t.Fatalf("List after reconnect = %#v, %v", items, err)
	}
	got, err := connection.Workspaces().Get(context.Background(), first.ID)
	if err != nil || got != first {
		t.Fatalf("Get = %#v, %v; want %#v", got, err, first)
	}
	if err := connection.Workspaces().Close(context.Background(), first.ID); err != nil {
		t.Fatalf("Close workspace: %v", err)
	}
	if err := connection.Workspaces().Close(context.Background(), first.ID); err != nil {
		t.Fatalf("idempotent Close workspace: %v", err)
	}
	if _, err := connection.Workspaces().Get(context.Background(), first.ID); !errors.Is(err, client.ErrWorkspaceNotFound) {
		t.Fatalf("Get closed error = %v, want ErrWorkspaceNotFound", err)
	}
	items, err = connection.Workspaces().List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("List after close = %#v, %v", items, err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()

	if err := connection.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	_ = connection.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after Shutdown RPC")
	}

	assertEndpointRemoved(t, endpoint)
}

func TestServeRejectsUnsupportedEndpoint(t *testing.T) {
	_, err := executeCommand(context.Background(), "test-version", "serve", "--endpoint", "tcp://127.0.0.1:50051")
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("serve error = %v, want unsupported scheme", err)
	}
}

func TestDefaultCompletionCommandIsDisabled(t *testing.T) {
	root := newRootCommand("test-version")
	if command, _, err := root.Find([]string{"completion"}); err == nil && command.Name() == "completion" {
		t.Fatal("default completion command is enabled")
	}
}

func executeCommand(ctx context.Context, version string, args ...string) (string, error) {
	var output bytes.Buffer
	root := newRootCommand(version)
	root.SetOut(&output)
	root.SetErr(&output)

	err := execute(ctx, root, args)
	return output.String(), err
}

func waitForClient(t *testing.T, endpoint client.Endpoint) *client.Client {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		connection, err := client.Dial(ctx, client.WithEndpoint(endpoint))
		cancel()
		if err == nil {
			return connection
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("daemon did not become available")

	return nil
}
