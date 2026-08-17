package main

import (
	"context"
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

func TestServeRejectsUnsupportedEndpoint(t *testing.T) {
	_, err := executeCommand(context.Background(), "test-version", "serve", "--endpoint", "tcp://127.0.0.1:50051")
	if err == nil || !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("serve error = %v, want unsupported scheme", err)
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
