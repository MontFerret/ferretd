package transport

import "errors"

var (
	// ErrEndpointInUse reports that another process is listening at an endpoint.
	ErrEndpointInUse = errors.New("endpoint is already in use")
	// ErrInvalidEndpoint reports an unsupported or malformed endpoint.
	ErrInvalidEndpoint = errors.New("invalid endpoint")
)
