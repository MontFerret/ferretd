package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
)

func TestOlderDaemonRemainsCompatibleWithoutDiscovery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	old := &olderDaemonServer{negotiations: make(chan APIVersion, 2)}
	server := grpc.NewServer()
	daemonv1.RegisterDaemonServiceServer(server, old)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()

		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	endpoint, err := ParseEndpoint("tcp://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	c, err := Dial(ctx, WithEndpoint(endpoint), WithBearerToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = c.Close() })

	if negotiated := <-old.negotiations; negotiated != (APIVersion{Major: 1, Minor: 3}) {
		t.Fatalf("negotiated = %+v", negotiated)
	}

	result, err := c.ListRuntimes(ctx)
	if !errors.Is(err, ErrUnsupportedOperation) || status.Code(err) != codes.Unimplemented || len(result.Runtimes) != 0 {
		t.Fatalf("discovery = %+v, %v", result, err)
	}

	info, err := c.Info(ctx)
	if err != nil || info.APIVersion != (APIVersion{Major: 1, Minor: 1}) {
		t.Fatalf("Info = %+v, %v", info, err)
	}

	if err := c.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
