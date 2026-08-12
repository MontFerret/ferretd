package grpc

import (
	"errors"

	grpcgo "google.golang.org/grpc"
)

// IsStoppedError reports the normal error returned when a gRPC server stops.
func IsStoppedError(err error) bool {
	return errors.Is(err, grpcgo.ErrServerStopped)
}
