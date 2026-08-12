package client

import (
	"fmt"

	"github.com/MontFerret/ferretd/internal/transport"
)

// Endpoint identifies a local daemon transport.
type Endpoint struct {
	Network string
	Address string
}

// Discover returns the deterministic endpoint for the current user.
func Discover() (Endpoint, error) {
	endpoint, err := transport.DefaultEndpoint()
	if err != nil {
		return Endpoint{}, fmt.Errorf("discover daemon endpoint: %w", err)
	}

	return fromTransportEndpoint(endpoint), nil
}

// ParseEndpoint parses a supported endpoint URL for the current platform.
func ParseEndpoint(value string) (Endpoint, error) {
	endpoint, err := transport.ParseEndpoint(value)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}

	return fromTransportEndpoint(endpoint), nil
}

// String returns the endpoint's URL form.
func (e Endpoint) String() string {
	return e.transportEndpoint().String()
}

func (e Endpoint) transportEndpoint() transport.Endpoint {
	return transport.Endpoint{Network: e.Network, Address: e.Address}
}

func fromTransportEndpoint(endpoint transport.Endpoint) Endpoint {
	return Endpoint{Network: endpoint.Network, Address: endpoint.Address}
}
