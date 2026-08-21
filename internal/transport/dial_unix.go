//go:build !windows

package transport

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
)

func dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Network != NetworkUnix || !filepath.IsAbs(endpoint.Address) {
		return nil, fmt.Errorf("%w: expected an absolute unix endpoint", ErrInvalidEndpoint)
	}

	var dialer net.Dialer

	return dialer.DialContext(ctx, "unix", endpoint.Address)
}
