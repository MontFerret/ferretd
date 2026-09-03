package client

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

// WithBearerToken authenticates each RPC to an authenticated local endpoint.
func WithBearerToken(token string) Option {
	return func(options *dialOptions) error {
		if token == "" {
			return ErrInvalidBearerToken
		}

		options.bearerToken = token

		return nil
	}
}

func configuredEndpoint(options dialOptions) (Endpoint, error) {
	if options.endpoint != nil {
		return *options.endpoint, nil
	}

	return Discover()
}
