package client

import (
	"fmt"

	"github.com/MontFerret/ferretd/internal/transport"
)

// Endpoint identifies a local daemon transport.
type Endpoint struct {
	value string
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
	return e.value
}

func (e Endpoint) transportEndpoint() (transport.Endpoint, error) {
	endpoint, err := transport.ParseEndpoint(e.value)
	if err != nil {
		return transport.Endpoint{}, fmt.Errorf("%w: %v", ErrInvalidEndpoint, err)
	}

	return endpoint, nil
}

func fromTransportEndpoint(endpoint transport.Endpoint) Endpoint {
	return Endpoint{value: endpoint.String()}
}
