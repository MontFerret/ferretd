//go:build windows

package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// DefaultEndpoint returns the deterministic endpoint for the current user.
func DefaultEndpoint() (Endpoint, error) {
	return Endpoint{Network: "npipe", Address: `\\.\pipe\ferretd`}, nil
}

// Listen creates a private local listener for the endpoint.
func Listen(endpoint Endpoint) (net.Listener, error) {
	if err := validateNamedPipe(endpoint); err != nil {
		return nil, err
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("resolve current user SID: %w", err)
	}

	securityDescriptor := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", user.User.Sid.String())
	listener, err := winio.ListenPipe(endpoint.Address, &winio.PipeConfig{
		SecurityDescriptor: securityDescriptor,
	})
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, fmt.Errorf("%w: %v", ErrEndpointInUse, err)
		}

		return nil, fmt.Errorf("listen on %s: %w", endpoint.String(), err)
	}

	return listener, nil
}

func dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if err := validateNamedPipe(endpoint); err != nil {
		return nil, err
	}

	return winio.DialPipeContext(ctx, endpoint.Address)
}

func validateNamedPipe(endpoint Endpoint) error {
	const prefix = `\\.\pipe\`

	if endpoint.Network != "npipe" ||
		!strings.HasPrefix(strings.ToLower(endpoint.Address), prefix) ||
		len(endpoint.Address) == len(prefix) {
		return fmt.Errorf("%w: expected a local named-pipe endpoint", ErrInvalidEndpoint)
	}

	return nil
}
