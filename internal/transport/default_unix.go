//go:build !windows

package transport

import (
	"fmt"
	"os"
	"path/filepath"
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
		Network: NetworkUnix,
		Address: filepath.Join(runtimeDirectory, "ferret", "ferretd.sock"),
	}, nil
}
