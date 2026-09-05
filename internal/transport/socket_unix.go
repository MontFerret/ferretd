//go:build !windows

package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

func ensureSocketPathAvailable(path string) error {
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
		//nolint:errorlint // Preserve endpoint contention as the sole error classification.
		return fmt.Errorf("%w: inspect existing socket: %v", ErrEndpointInUse, dialErr)
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale endpoint socket: %w", err)
	}

	return nil
}
