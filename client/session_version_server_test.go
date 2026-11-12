package client

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
)

type sessionVersionServer struct {
	daemonv1.UnimplementedDaemonServiceServer
	executionv1.UnimplementedExecutionServiceServer
	minor   atomic.Uint32
	infos   atomic.Int32
	creates atomic.Int32
}

func newSessionVersionServer(t *testing.T, minor uint32) (*sessionVersionServer, *Client) {
	t.Helper()
	s := &sessionVersionServer{}
	s.minor.Store(minor)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	server := grpc.NewServer()
	daemonv1.RegisterDaemonServiceServer(server, s)
	executionv1.RegisterExecutionServiceServer(server, s)
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

	connection, err := Dial(ctx, WithEndpoint(endpoint), WithBearerToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = connection.Close() })

	return s, connection
}

func (s *sessionVersionServer) GetInfo(context.Context, *daemonv1.GetInfoRequest) (*daemonv1.GetInfoResponse, error) {
	s.infos.Add(1)

	return &daemonv1.GetInfoResponse{ServerInfo: &daemonv1.ServerInfo{ApiVersion: &daemonv1.ApiVersion{Major: 1, Minor: s.minor.Load()}}}, nil
}

func (s *sessionVersionServer) CreateSession(_ context.Context, request *executionv1.CreateSessionRequest) (*executionv1.CreateSessionResponse, error) {
	s.creates.Add(1)

	name := request.RuntimeName
	if s.minor.Load() >= 3 && name == "" {
		name = "local"
	}

	return &executionv1.CreateSessionResponse{Session: sessionResponse(name)}, nil
}
