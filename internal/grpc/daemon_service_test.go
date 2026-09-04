package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

func TestDaemonGetInfoNegotiatesAPIVersion(t *testing.T) {
	service := mustNewDaemonService(t, "v1.2.3", "instance", func() {})

	for _, minor := range []uint32{1, 2, 99} {
		response, err := service.GetInfo(context.Background(), &daemonv1.GetInfoRequest{
			ClientApi: &daemonv1.ApiVersion{Major: 1, Minor: minor},
		})
		if err != nil {
			t.Fatalf("GetInfo with client API 1.%d: %v", minor, err)
		}
		if response.ServerInfo.Version != "v1.2.3" || response.ServerInfo.InstanceId != "instance" {
			t.Fatalf("server info = %#v", response.ServerInfo)
		}
		if got := response.ServerInfo.ApiVersion; got.Major != 1 || got.Minor != 2 {
			t.Fatalf("API version = %#v, want 1.2", got)
		}
	}
}

func TestDaemonGetInfoRejectsAPIMajorMismatch(t *testing.T) {
	service := mustNewDaemonService(t, "dev", "instance", func() {})
	clientAPI := &daemonv1.ApiVersion{Major: 2, Minor: 3}

	_, err := service.GetInfo(context.Background(), &daemonv1.GetInfoRequest{ClientApi: clientAPI})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("GetInfo code = %v, want FailedPrecondition", status.Code(err))
	}

	grpcStatus := status.Convert(err)
	if len(grpcStatus.Details()) != 1 {
		t.Fatalf("details = %#v, want one compatibility detail", grpcStatus.Details())
	}
	detail, ok := grpcStatus.Details()[0].(*daemonv1.ApiCompatibilityError)
	if !ok {
		t.Fatalf("detail type = %T, want ApiCompatibilityError", grpcStatus.Details()[0])
	}
	if detail.ClientApi.Major != 2 || detail.ServerApi.Major != 1 {
		t.Fatalf("compatibility detail = %#v", detail)
	}
}

func TestDaemonGetInfoRequiresClientVersion(t *testing.T) {
	service := mustNewDaemonService(t, "dev", "instance", func() {})

	_, err := service.GetInfo(context.Background(), &daemonv1.GetInfoRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetInfo code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestDaemonShutdownIsIdempotent(t *testing.T) {
	calls := 0
	service := mustNewDaemonService(t, "dev", "instance", func() {
		calls++
	})

	for range 2 {
		if _, err := service.Shutdown(context.Background(), &daemonv1.ShutdownRequest{}); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("shutdown callback calls = %d, want 1", calls)
	}
}
