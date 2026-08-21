package grpc

import (
	grpcgo "google.golang.org/grpc"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	"github.com/MontFerret/ferretd/internal/exec"
)

func sendExecutionEvent(
	stream grpcgo.ServerStreamingServer[executionv1.WatchExecutionResponse],
	event exec.Event,
) error {
	encoded, err := toProtoExecutionEvent(event)
	if err != nil {
		return toExecutionStatusError(err)
	}

	return stream.Send(encoded)
}

func subscriptionStatusError(errorsChannel <-chan error) error {
	for watchErr := range errorsChannel {
		if watchErr != nil {
			return toExecutionStatusError(watchErr)
		}
	}

	return nil
}
