package client

import "context"

const authorizationMetadataKey = "authorization"

type bearerCredentials struct {
	authorization string
}

func newBearerCredentials(token string) bearerCredentials {
	return bearerCredentials{authorization: "Bearer " + token}
}

func (c bearerCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{authorizationMetadataKey: c.authorization}, nil
}

func (bearerCredentials) RequireTransportSecurity() bool {
	return false
}
