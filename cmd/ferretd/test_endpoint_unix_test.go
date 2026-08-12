//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/client"
)

func testClientEndpoint(t *testing.T) client.Endpoint {
	t.Helper()

	directory, err := os.MkdirTemp("/tmp", "ferretd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	return client.Endpoint{
		Network: "unix",
		Address: filepath.Join(directory, "ferretd.sock"),
	}
}

func assertEndpointRemoved(t *testing.T, endpoint client.Endpoint) {
	t.Helper()

	if _, err := os.Lstat(endpoint.Address); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("endpoint remains after shutdown: %v", err)
	}
}
