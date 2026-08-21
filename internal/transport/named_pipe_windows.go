//go:build windows

package transport

import (
	"fmt"
	"strings"
)

func validateNamedPipe(endpoint Endpoint) error {
	const prefix = `\\.\pipe\`

	if endpoint.Network != NetworkNamedPipe ||
		!strings.HasPrefix(strings.ToLower(endpoint.Address), prefix) ||
		len(endpoint.Address) == len(prefix) {
		return fmt.Errorf("%w: expected a local named-pipe endpoint", ErrInvalidEndpoint)
	}

	return nil
}
