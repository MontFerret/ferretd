package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestNewRequiresDomainServices(t *testing.T) {
	workspaces := workspace.New()
	executions := mustNewExecutionManager(t, workspaces)
	tests := []struct {
		name string
		new  func() (*Server, error)
		want error
	}{
		{
			name: "workspace manager",
			new:  func() (*Server, error) { return New(nil, executions, "dev", "instance", nil, Options{}) },
			want: errNilWorkspaceManager,
		},
		{
			name: "execution manager",
			new:  func() (*Server, error) { return New(workspaces, nil, "dev", "instance", nil, Options{}) },
			want: errNilExecutionManager,
		},
		{
			name: "shutdown callback",
			new:  func() (*Server, error) { return New(workspaces, executions, "dev", "instance", nil, Options{}) },
			want: errNilShutdown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := tt.new()
			if server != nil {
				t.Fatal("New returned a server with a nil domain dependency")
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("New error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestServerHealthTransitions(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	workspaces := workspace.New()
	server := mustNewServer(t, workspaces, mustNewExecutionManager(t, workspaces))
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	healthClient := healthv1.NewHealthClient(connection)
	serviceNames := []string{
		"",
		daemonv1.DaemonService_ServiceDesc.ServiceName,
		workspacev1.WorkspaceService_ServiceDesc.ServiceName,
		executionv1.ExecutionService_ServiceDesc.ServiceName,
	}
	assertHealthStatus(t, ctx, healthClient, serviceNames, healthv1.HealthCheckResponse_NOT_SERVING)

	server.SetServing()
	assertHealthStatus(t, ctx, healthClient, serviceNames, healthv1.HealthCheckResponse_SERVING)

	server.SetNotServing()
	assertHealthStatus(t, ctx, healthClient, serviceNames, healthv1.HealthCheckResponse_NOT_SERVING)

	if err := server.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil && !IsStoppedError(err) {
		t.Fatalf("Serve error = %v, want normal stop", err)
	}
}

func TestServerStopForcesAfterDeadline(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	workspaces := workspace.New()
	server := mustNewServer(t, workspaces, mustNewExecutionManager(t, workspaces))
	server.SetServing()
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
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

	watch, err := healthv1.NewHealthClient(connection).Watch(
		context.Background(),
		&healthv1.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if _, err := watch.Recv(); err != nil {
		t.Fatalf("initial Watch response: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := server.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want context.DeadlineExceeded", err)
	}
	if err := <-done; err != nil && !IsStoppedError(err) {
		t.Fatalf("Serve error = %v, want normal stop", err)
	}
}

func assertHealthStatus(
	t *testing.T,
	ctx context.Context,
	client healthv1.HealthClient,
	services []string,
	want healthv1.HealthCheckResponse_ServingStatus,
) {
	t.Helper()

	for _, service := range services {
		response, err := client.Check(ctx, &healthv1.HealthCheckRequest{Service: service})
		if err != nil {
			t.Fatalf("Check(%q): %v", service, err)
		}
		if response.Status != want {
			t.Fatalf("health(%q) = %v, want %v", service, response.Status, want)
		}
	}
}
