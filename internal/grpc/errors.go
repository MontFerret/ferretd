package grpc

import (
	"errors"

	grpcgo "google.golang.org/grpc"
)

var (
	errNilWorkspaceManager = errors.New("grpc: nil workspace manager")
	errNilExecutionManager = errors.New("grpc: nil execution manager")
)

// IsStoppedError reports the normal error returned when a gRPC server stops.
func IsStoppedError(err error) bool {
	return errors.Is(err, grpcgo.ErrServerStopped)
}
