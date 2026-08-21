//go:build windows

package transport

// DefaultEndpoint returns the deterministic endpoint for the current user.
func DefaultEndpoint() (Endpoint, error) {
	return Endpoint{Network: NetworkNamedPipe, Address: `\\.\pipe\ferretd`}, nil
}
