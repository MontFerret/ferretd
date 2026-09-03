//go:build windows

package transport

import (
	"errors"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func listenLocal(endpoint Endpoint) (net.Listener, error) {
	if err := validateNamedPipe(endpoint); err != nil {
		return nil, err
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("resolve current user SID: %w", err)
	}

	securityDescriptor := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;%s)", user.User.Sid.String())
	listener, err := winio.ListenPipe(endpoint.Address, &winio.PipeConfig{SecurityDescriptor: securityDescriptor})
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, fmt.Errorf("%w: %v", ErrEndpointInUse, err)
		}

		return nil, fmt.Errorf("listen on %s: %w", endpoint.String(), err)
	}

	return listener, nil
}
