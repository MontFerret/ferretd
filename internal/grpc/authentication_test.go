package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestBearerAuthenticationProtectsUnaryAndStreamingRPCs(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	workspaces := workspace.New()

	server, err := New(
		workspaces,
		mustNewExecutionManager(t, workspaces),
		"dev",
		"instance",
		func() {},
		Options{BearerToken: "correct-token"},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	server.SetServing()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	connection, err := grpcgo.NewClient(
		"passthrough:///bufnet",
		grpcgo.WithTransportCredentials(insecure.NewCredentials()),
		grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	t.Cleanup(func() { _ = connection.Close() })

	client := healthv1.NewHealthClient(connection)
	for _, authorization := range []string{"", "Bearer wrong-token"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)

		if authorization != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, authorizationMetadataKey, authorization)
		}

		_, checkErr := client.Check(ctx, &healthv1.HealthCheckRequest{})
		if status.Code(checkErr) != codes.Unauthenticated {
			t.Fatalf("Check authorization %q error = %v, want Unauthenticated", authorization, checkErr)
		}

		watch, watchErr := client.Watch(ctx, &healthv1.HealthCheckRequest{})
		if watchErr == nil {
			_, watchErr = watch.Recv()
		}

		if status.Code(watchErr) != codes.Unauthenticated {
			t.Fatalf("Watch authorization %q error = %v, want Unauthenticated", authorization, watchErr)
		}

		cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	ctx = metadata.AppendToOutgoingContext(ctx, authorizationMetadataKey, "Bearer correct-token")

	response, err := client.Check(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("authenticated Check: %v", err)
	}

	if response.Status != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("health status = %v, want SERVING", response.Status)
	}

	watch, err := client.Watch(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("authenticated Watch: %v", err)
	}

	response, err = watch.Recv()
	if err != nil {
		t.Fatalf("authenticated Watch Recv: %v", err)
	}

	if response.Status != healthv1.HealthCheckResponse_SERVING {
		t.Fatalf("watched health status = %v, want SERVING", response.Status)
	}

	cancel()

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()

	if err := server.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := <-serveDone; err != nil && !IsStoppedError(err) {
		t.Fatalf("Serve error = %v, want normal stop", err)
	}
}
