//go:build !windows

package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// DefaultEndpoint returns the deterministic endpoint for the current user.
func DefaultEndpoint() (Endpoint, error) {
	runtimeDirectory := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDirectory == "" || !filepath.IsAbs(runtimeDirectory) {
		cacheDirectory, err := os.UserCacheDir()
		if err != nil {
			return Endpoint{}, fmt.Errorf("resolve user cache directory: %w", err)
		}

		runtimeDirectory = cacheDirectory
	}

	return Endpoint{
		Network: "unix",
		Address: filepath.Join(runtimeDirectory, "ferret", "ferretd.sock"),
	}, nil
}

// Listen creates a private local listener for the endpoint.
func Listen(endpoint Endpoint) (net.Listener, error) {
	if endpoint.Network != "unix" || !filepath.IsAbs(endpoint.Address) {
		return nil, fmt.Errorf("%w: expected an absolute unix endpoint", ErrInvalidEndpoint)
	}

	directory := filepath.Dir(endpoint.Address)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create endpoint directory: %w", err)
	}

	if err := prepareSocket(endpoint.Address); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", endpoint.Address)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return nil, fmt.Errorf("%w: %v", ErrEndpointInUse, err)
		}

		return nil, fmt.Errorf("listen on %s: %w", endpoint.String(), err)
	}

	if err := os.Chmod(endpoint.Address, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(endpoint.Address)

		return nil, fmt.Errorf("secure endpoint socket: %w", err)
	}

	return &cleanupListener{
		Listener: listener,
		path:     endpoint.Address,
	}, nil
}

func dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Network != "unix" || !filepath.IsAbs(endpoint.Address) {
		return nil, fmt.Errorf("%w: expected an absolute unix endpoint", ErrInvalidEndpoint)
	}

	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", endpoint.Address)
}

func prepareSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect endpoint: %w", err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%w: endpoint path is not a socket", ErrEndpointInUse)
	}

	connection, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()

		return ErrEndpointInUse
	}

	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect existing socket: %v", ErrEndpointInUse, dialErr)
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale endpoint socket: %w", err)
	}

	return nil
}

type cleanupListener struct {
	net.Listener
	path string
	once sync.Once
	err  error
}

func (l *cleanupListener) Close() error {
	l.once.Do(func() {
		closeErr := l.Listener.Close()
		removeErr := os.Remove(l.path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}

		l.err = errors.Join(closeErr, removeErr)
	})

	return l.err
}
