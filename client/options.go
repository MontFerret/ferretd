package client

import "github.com/MontFerret/ferretd/internal/transport"

type (
	// Option configures a daemon connection.
	Option func(*dialOptions) error

	dialOptions struct {
		endpoint    *Endpoint
		bearerToken string
	}
)

// WithEndpoint selects an explicit endpoint instead of discovery.
func WithEndpoint(endpoint Endpoint) Option {
	return func(options *dialOptions) error {
		if endpoint.value == "" {
			return ErrInvalidEndpoint
		}

		options.endpoint = &endpoint

		return nil
	}
}

// WithBearerToken authenticates each RPC to a TCP endpoint.
func WithBearerToken(token string) Option {
	return func(options *dialOptions) error {
		if token == "" {
			return ErrInvalidBearerToken
		}

		options.bearerToken = token

		return nil
	}
}

func (o dialOptions) resolvedEndpoint() (transport.Endpoint, error) {
	var endpoint Endpoint
	if o.endpoint != nil {
		endpoint = *o.endpoint
	} else {
		var err error

		endpoint, err = Discover()
		if err != nil {
			return transport.Endpoint{}, err
		}
	}

	result, err := endpoint.transportEndpoint()
	if err != nil {
		return transport.Endpoint{}, err
	}

	if result.Network == transport.NetworkTCP && o.bearerToken == "" {
		return transport.Endpoint{}, ErrBearerTokenRequired
	}

	if result.Network != transport.NetworkTCP && o.bearerToken != "" {
		return transport.Endpoint{}, ErrBearerTokenUnsupported
	}

	return result, nil
}
