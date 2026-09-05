package client

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

func TestMapCompatibilityError(t *testing.T) {
	grpcStatus := status.New(codes.FailedPrecondition, "incompatible")

	withDetails, err := grpcStatus.WithDetails(&daemonv1.ApiCompatibilityError{
		ClientApi: &daemonv1.ApiVersion{Major: 2, Minor: 1},
		ServerApi: &daemonv1.ApiVersion{Major: 1, Minor: 0},
	})
	if err != nil {
		t.Fatalf("WithDetails: %v", err)
	}

	mapped := mapError(context.Background(), withDetails.Err())
	if !errors.Is(mapped, ErrIncompatibleAPI) {
		t.Fatalf("mapError = %v, want ErrIncompatibleAPI", mapped)
	}

	var compatibility *IncompatibleAPIError
	if !errors.As(mapped, &compatibility) {
		t.Fatalf("mapError type = %T, want IncompatibleAPIError", mapped)
	}

	if compatibility.Client.Major != 2 || compatibility.Server.Major != 1 {
		t.Fatalf("compatibility = %#v", compatibility)
	}
}

func TestMapErrorPreservesContextIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := mapError(ctx, status.Error(codes.Unavailable, "closing")); !errors.Is(err, context.Canceled) {
		t.Fatalf("mapError = %v, want context.Canceled", err)
	}
}

func TestMapErrorClassifiesGenericFailures(t *testing.T) {
	tests := []struct {
		name string
		code codes.Code
		want error
	}{
		{name: "canceled", code: codes.Canceled, want: context.Canceled},
		{name: "deadline", code: codes.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "unavailable", code: codes.Unavailable, want: ErrDaemonUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := mapError(context.Background(), status.Error(tt.code, tt.name))
			if !errors.Is(mapped, tt.want) {
				t.Fatalf("mapError = %v, want %v", mapped, tt.want)
			}
		})
	}
}

func TestMapErrorLeavesAmbiguousCodesUnclassified(t *testing.T) {
	tests := []struct {
		name      string
		code      codes.Code
		notWanted error
	}{
		{name: "invalid argument", code: codes.InvalidArgument, notWanted: ErrInvalidWorkspaceRoot},
		{name: "not found", code: codes.NotFound, notWanted: ErrWorkspaceNotFound},
		{name: "failed precondition", code: codes.FailedPrecondition, notWanted: ErrIncompatibleAPI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped := mapError(context.Background(), status.Error(tt.code, tt.name))
			if errors.Is(mapped, tt.notWanted) {
				t.Fatalf("mapError = %v, did not want %v", mapped, tt.notWanted)
			}

			if status.Code(mapped) != tt.code {
				t.Fatalf("mapError code = %v, want %v", status.Code(mapped), tt.code)
			}
		})
	}
}

func TestDialPreservesPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Dial(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial error = %v, want context.Canceled", err)
	}

	if errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("Dial error = %v, did not attempt a daemon connection", err)
	}
}
