package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const authorizationMetadataKey = "authorization"

type bearerAuthentication struct {
	expected [sha256.Size]byte
}

func newBearerAuthentication(token string) *bearerAuthentication {
	return &bearerAuthentication{expected: sha256.Sum256([]byte("Bearer " + token))}
}

func (a *bearerAuthentication) interceptUnary(
	ctx context.Context,
	request any,
	info *grpcgo.UnaryServerInfo,
	handler grpcgo.UnaryHandler,
) (any, error) {
	if err := a.authenticate(ctx); err != nil {
		return nil, err
	}

	return handler(ctx, request)
}

func (a *bearerAuthentication) interceptStream(
	service any,
	stream grpcgo.ServerStream,
	info *grpcgo.StreamServerInfo,
	handler grpcgo.StreamHandler,
) error {
	if err := a.authenticate(stream.Context()); err != nil {
		return err
	}

	return handler(service, stream)
}

func (a *bearerAuthentication) authenticate(ctx context.Context) error {
	values := metadata.ValueFromIncomingContext(ctx, authorizationMetadataKey)
	if len(values) != 1 {
		return status.Error(codes.Unauthenticated, "valid bearer authentication is required")
	}

	actual := sha256.Sum256([]byte(values[0]))
	if subtle.ConstantTimeCompare(actual[:], a.expected[:]) != 1 {
		return status.Error(codes.Unauthenticated, "valid bearer authentication is required")
	}

	return nil
}
