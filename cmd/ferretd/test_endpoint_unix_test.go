//go:build !windows

package main

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/client"
)

func testClientEndpoint(t *testing.T) client.Endpoint {
	t.Helper()

	directory, err := os.MkdirTemp("/var/tmp", "ferretd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	endpoint, err := client.ParseEndpoint((&url.URL{
		Scheme: "unix",
		Path:   filepath.Join(directory, "ferretd.sock"),
	}).String())
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	return endpoint
}

func assertEndpointRemoved(t *testing.T, endpoint client.Endpoint) {
	t.Helper()

	parsed, err := url.Parse(endpoint.String())
	if err != nil {
		t.Fatalf("parse endpoint URL: %v", err)
	}

	if _, err := os.Lstat(parsed.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("endpoint remains after shutdown: %v", err)
	}
}
