//go:build !windows

package transport

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "unix", value: "unix:///tmp/ferret/../ferretd.sock", want: "unix:///tmp/ferretd.sock"},
		{name: "relative", value: "unix://relative", want: "invalid"},
		{name: "query", value: "unix:///tmp/ferretd.sock?x=1", want: "invalid"},
		{name: "tcp", value: "tcp://127.0.0.1:50051", want: "invalid"},
		{name: "named pipe", value: "npipe:////./pipe/ferretd", want: "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, err := ParseEndpoint(tt.value)
			if tt.want == "invalid" {
				if !errors.Is(err, ErrInvalidEndpoint) {
					t.Fatalf("ParseEndpoint error = %v, want ErrInvalidEndpoint", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if got := endpoint.String(); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultEndpointUsesXDGRuntimeDirectory(t *testing.T) {
	runtimeDirectory := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDirectory)

	endpoint, err := DefaultEndpoint()
	if err != nil {
		t.Fatalf("DefaultEndpoint: %v", err)
	}

	want := filepath.Join(runtimeDirectory, "ferret", "ferretd.sock")
	if endpoint.Network != NetworkUnix || endpoint.Address != want {
		t.Fatalf("endpoint = %#v, want unix %q", endpoint, want)
	}
}

func TestDefaultEndpointIgnoresRelativeXDGRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "relative")

	endpoint, err := DefaultEndpoint()
	if err != nil {
		t.Fatalf("DefaultEndpoint: %v", err)
	}
	if !filepath.IsAbs(endpoint.Address) {
		t.Fatalf("endpoint address = %q, want absolute cache fallback", endpoint.Address)
	}
}

func TestNamedPipeEndpointString(t *testing.T) {
	endpoint := Endpoint{Network: NetworkNamedPipe, Address: `\\.\pipe\ferretd`}
	if got, want := endpoint.String(), "npipe:////./pipe/ferretd"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
}

func TestListenDialAndCleanup(t *testing.T) {
	endpoint := Endpoint{Network: NetworkUnix, Address: filepath.Join(shortTempDir(t), "ferret", "ferretd.sock")}
	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	info, err := os.Stat(endpoint.Address)
	if err != nil {
		t.Fatalf("stat endpoint: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %#o, want 0600", got)
	}

	directoryInfo, err := os.Stat(filepath.Dir(endpoint.Address))
	if err != nil {
		t.Fatalf("stat endpoint directory: %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("endpoint directory mode = %#o, want 0700", got)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	connection, err := Dial(ctx, endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = connection.Close()

	serverConnection := <-accepted
	if serverConnection != nil {
		_ = serverConnection.Close()
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if _, err := os.Lstat(endpoint.Address); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("endpoint remains after close: %v", err)
	}
}

func TestListenRefusesActiveOrNonSocketEndpoint(t *testing.T) {
	t.Run("active", func(t *testing.T) {
		endpoint := Endpoint{Network: NetworkUnix, Address: filepath.Join(shortTempDir(t), "ferretd.sock")}
		listener, err := Listen(endpoint)
		if err != nil {
			t.Fatalf("first Listen: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })

		_, err = Listen(endpoint)
		if !errors.Is(err, ErrEndpointInUse) {
			t.Fatalf("second Listen error = %v, want ErrEndpointInUse", err)
		}
	})

	t.Run("regular file", func(t *testing.T) {
		endpoint := Endpoint{Network: NetworkUnix, Address: filepath.Join(shortTempDir(t), "ferretd.sock")}
		if err := os.WriteFile(endpoint.Address, nil, 0o600); err != nil {
			t.Fatalf("write collision: %v", err)
		}

		_, err := Listen(endpoint)
		if !errors.Is(err, ErrEndpointInUse) {
			t.Fatalf("Listen error = %v, want ErrEndpointInUse", err)
		}
	})
}

func TestListenReclaimsStaleSocket(t *testing.T) {
	endpoint := Endpoint{Network: NetworkUnix, Address: filepath.Join(shortTempDir(t), "ferretd.sock")}
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint.Address, Net: "unix"})
	if err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatalf("close stale socket: %v", err)
	}

	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()

	directory, err := os.MkdirTemp("/tmp", "ferretd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	return directory
}
