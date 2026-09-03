package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/client"
)

func TestServeStopsOnCancellation(t *testing.T) {
	endpoint := testClientEndpoint(t)
	ctx, cancel := context.WithCancel(context.Background())
	type serveResult struct {
		diagnostics string
		err         error
	}
	done := make(chan serveResult, 1)

	go func() {
		diagnostics, err := executeCommand(ctx, "test-version", "serve", "--endpoint", endpoint.String())
		done <- serveResult{diagnostics: diagnostics, err: err}
	}()

	connection := waitForClient(t, endpoint)
	_ = connection.Close()
	cancel()

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("execute serve: %v", result.err)
		}

		var diagnostic map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.diagnostics)), &diagnostic); err != nil {
			t.Fatalf("decode serve diagnostics %q: %v", result.diagnostics, err)
		}
		if diagnostic["level"] != "info" || diagnostic["event"] != "ferretd.ready" ||
			diagnostic["message"] != "ferretd started" ||
			diagnostic["endpoint"] != endpoint.String() || diagnostic["version"] != "test-version" {
			t.Fatalf("serve diagnostic = %#v", diagnostic)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not stop after context cancellation")
	}

	assertEndpointRemoved(t, endpoint)
}

func TestServeEndToEnd(t *testing.T) {
	endpoint := testClientEndpoint(t)
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan struct{})
	var serveErr error

	go func() {
		defer close(serveDone)

		_, serveErr = executeCommand(
			serveCtx,
			"test-version",
			"serve",
			"--endpoint",
			endpoint.String(),
		)
	}()

	t.Cleanup(func() {
		cancelServe()

		select {
		case <-serveDone:
			if serveErr != nil {
				t.Errorf("execute serve during cleanup: %v", serveErr)
			}
		case <-time.After(2 * time.Second):
			t.Error("serve did not stop during cleanup")
		}

		assertEndpointRemoved(t, endpoint)
	})

	connection := waitForClient(t, endpoint)
	firstConnection := connection
	t.Cleanup(func() { _ = firstConnection.Close() })

	info, err := connection.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if info.Version != "test-version" || info.InstanceID == "" ||
		info.APIVersion != (client.APIVersion{Major: 1, Minor: 1}) {
		t.Fatalf("server info = %#v", info)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	root, err := os.MkdirTemp(cwd, "ferretd-workspace-")
	if err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

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
	secondConnection := connection
	t.Cleanup(func() { _ = secondConnection.Close() })

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
	case <-serveDone:
		if serveErr != nil {
			t.Fatalf("execute serve: %v", serveErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after Shutdown RPC")
	}

	assertEndpointRemoved(t, endpoint)
}

func TestServeValidatesTCPAuthentication(t *testing.T) {
	tests := []struct {
		name             string
		args             []string
		environment      string
		environmentValue string
		want             string
	}{
		{
			name: "missing option",
			args: []string{"serve", "--endpoint", "tcp://127.0.0.1:0"},
			want: "requires --auth-token-env",
		},
		{
			name: "missing environment variable",
			args: []string{
				"serve", "--endpoint", "tcp://127.0.0.1:0", "--auth-token-env", "FERRETD_MISSING_TOKEN",
			},
			want: "is missing or empty",
		},
		{
			name: "empty environment variable",
			args: []string{
				"serve", "--endpoint", "tcp://127.0.0.1:0", "--auth-token-env", "FERRETD_EMPTY_TOKEN",
			},
			environment: "FERRETD_EMPTY_TOKEN",
			want:        "is missing or empty",
		},
		{
			name: "environment variable with surrounding whitespace",
			args: []string{
				"serve", "--endpoint", "tcp://127.0.0.1:0", "--auth-token-env", " FERRETD_TEST_TOKEN",
			},
			want: "without surrounding whitespace",
		},
		{
			name: "native endpoint",
			args: []string{
				"serve", "--endpoint", testClientEndpoint(t).String(), "--auth-token-env", "FERRETD_TEST_TOKEN",
			},
			environment:      "FERRETD_TEST_TOKEN",
			environmentValue: "test-token",
			want:             "only supported with a TCP endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.environment != "" {
				t.Setenv(tt.environment, tt.environmentValue)
			}

			_, err := executeCommand(context.Background(), "test-version", tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("serve error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestServeAuthenticatedTCP(t *testing.T) {
	const (
		tokenEnvironment = "FERRETD_TEST_TOKEN"
		token            = "test-token-that-must-not-be-logged"
	)
	t.Setenv(tokenEnvironment, token)

	diagnostics := newReadyDiagnosticWriter()
	root := newRootCommand("test-version")
	root.SetOut(diagnostics)
	root.SetErr(diagnostics)
	serveCtx, cancelServe := context.WithCancel(context.Background())
	t.Cleanup(cancelServe)

	done := make(chan error, 1)
	go func() {
		done <- execute(serveCtx, root, []string{
			"serve",
			"--endpoint", "tcp://127.0.0.1:0",
			"--auth-token-env", tokenEnvironment,
			"--log-level", "error",
		})
	}()

	var record []byte
	select {
	case record = <-diagnostics.records:
	case <-time.After(time.Second):
		t.Fatal("serve did not report readiness")
	}

	if bytes.Contains(record, []byte(token)) {
		t.Fatal("ready diagnostic contains the bearer token")
	}

	var ready struct {
		Event    string `json:"event"`
		Endpoint string `json:"endpoint"`
		Version  string `json:"version"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(record), &ready); err != nil {
		t.Fatalf("decode ready diagnostic %q: %v", record, err)
	}
	if ready.Event != "ferretd.ready" || ready.Version != "test-version" || ready.Message != "ferretd started" {
		t.Fatalf("ready diagnostic = %#v", ready)
	}

	endpoint, err := client.ParseEndpoint(ready.Endpoint)
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if endpoint.String() == "tcp://127.0.0.1:0" {
		t.Fatalf("reported endpoint = %q, want assigned port", endpoint.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	connection, err := client.Dial(
		ctx,
		client.WithEndpoint(endpoint),
		client.WithBearerToken(token),
	)
	cancel()
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	if err := connection.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		t.Fatalf("Shutdown: %v", err)
	}
	cancelShutdown()
	_ = connection.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execute serve: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not stop after Shutdown RPC")
	}
}

func TestServeRejectsNonEphemeralTCPListener(t *testing.T) {
	t.Setenv("FERRETD_TEST_TOKEN", "test-token")

	_, err := executeCommand(
		context.Background(),
		"test-version",
		"serve",
		"--endpoint",
		"tcp://127.0.0.1:50051",
		"--auth-token-env",
		"FERRETD_TEST_TOKEN",
	)
	if err == nil || !strings.Contains(err.Error(), "ephemeral port") {
		t.Fatalf("serve error = %v, want ephemeral-port rejection", err)
	}
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
