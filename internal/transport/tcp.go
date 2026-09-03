package transport

import (
	"context"
	"fmt"
	"net"
	"strconv"
)

func listenTCP(endpoint Endpoint) (*Listener, error) {
	if err := validateTCPEndpoint(endpoint, true); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp4", endpoint.Address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", endpoint.String(), err)
	}

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.Equal(net.IPv4(127, 0, 0, 1)) || address.Port == 0 {
		_ = listener.Close()

		return nil, fmt.Errorf("%w: TCP listener returned an invalid loopback address", ErrInvalidEndpoint)
	}

	bound := Endpoint{
		Network: NetworkTCP,
		Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(address.Port)),
	}

	return &Listener{Listener: listener, endpoint: bound}, nil
}

func dialTCP(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if err := validateTCPEndpoint(endpoint, false); err != nil {
		return nil, err
	}

	var dialer net.Dialer

	return dialer.DialContext(ctx, "tcp4", endpoint.Address)
}

func validateTCPEndpoint(endpoint Endpoint, listening bool) error {
	if endpoint.Network != NetworkTCP {
		return fmt.Errorf("%w: expected a TCP endpoint", ErrInvalidEndpoint)
	}

	host, portValue, err := net.SplitHostPort(endpoint.Address)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("%w: expected an IPv4 loopback TCP endpoint", ErrInvalidEndpoint)
	}

	port, err := strconv.ParseUint(portValue, 10, 16)
	if err != nil {
		return fmt.Errorf("%w: TCP endpoint contains an invalid port", ErrInvalidEndpoint)
	}

	if listening && port != 0 {
		return fmt.Errorf("%w: TCP listeners must use an ephemeral port", ErrInvalidEndpoint)
	}

	if !listening && port == 0 {
		return fmt.Errorf("%w: cannot dial an unassigned TCP port", ErrInvalidEndpoint)
	}

	return nil
}
