//go:build windows

package transport

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if err := validateNamedPipe(endpoint); err != nil {
		return nil, err
	}

	return winio.DialPipeContext(ctx, endpoint.Address)
}
