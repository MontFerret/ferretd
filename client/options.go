package client

// Option configures a daemon connection.
type Option func(*dialOptions) error

type dialOptions struct {
	endpoint *Endpoint
}

// WithEndpoint selects an explicit endpoint instead of discovery.
func WithEndpoint(endpoint Endpoint) Option {
	return func(options *dialOptions) error {
		parsed, err := ParseEndpoint(endpoint.String())
		if err != nil {
			return err
		}

		options.endpoint = &parsed

		return nil
	}
}

func configuredEndpoint(options dialOptions) (Endpoint, error) {
	if options.endpoint != nil {
		return *options.endpoint, nil
	}

	return Discover()
}
