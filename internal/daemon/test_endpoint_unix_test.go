//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/transport"
)

func testEndpoint(t testing.TB) transport.Endpoint {
	t.Helper()

	directory, err := os.MkdirTemp("/var/tmp", "ferretd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	return transport.Endpoint{
		Network: transport.NetworkUnix,
		Address: filepath.Join(directory, "ferretd.sock"),
	}
}
