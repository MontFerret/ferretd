//go:build !windows

package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type cleanupListener struct {
	net.Listener
	path string
	once sync.Once
	err  error
}

func listenLocal(endpoint Endpoint) (net.Listener, error) {
	if endpoint.Network != NetworkUnix || !filepath.IsAbs(endpoint.Address) {
		return nil, fmt.Errorf("%w: expected an absolute unix endpoint", ErrInvalidEndpoint)
	}

	directory := filepath.Dir(endpoint.Address)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create endpoint directory: %w", err)
	}

	if err := ensureSocketPathAvailable(endpoint.Address); err != nil {
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

	return &cleanupListener{Listener: listener, path: endpoint.Address}, nil
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
