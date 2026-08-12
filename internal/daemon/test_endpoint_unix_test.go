//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/transport"
)

func testEndpoint(t *testing.T) transport.Endpoint {
	t.Helper()

	directory, err := os.MkdirTemp("/tmp", "ferretd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	return transport.Endpoint{
		Network: "unix",
		Address: filepath.Join(directory, "ferretd.sock"),
	}
}
