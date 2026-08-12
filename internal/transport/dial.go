package transport

import (
	"context"
	"fmt"
	"net"
)

// Dial connects to an endpoint using the current platform's local transport.
func Dial(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	connection, err := dial(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", endpoint.String(), err)
	}

	return connection, nil
}
