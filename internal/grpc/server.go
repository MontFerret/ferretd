// Package grpc adapts daemon services to the gRPC protocol.
package grpc

import (
	"context"
	"net"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Server owns the gRPC transport adapter and its health state.
type Server struct {
	server *grpcgo.Server
	health *health.Server
}

// New constructs the daemon's gRPC adapter over the supplied domain services.
// It returns an error when either required service is nil.
func New(
	workspaces *workspace.Manager,
	executions *exec.Manager,
	version string,
	instanceID string,
	shutdown func(),
) (*Server, error) {
	workspaceAdapter, err := newWorkspaceService(workspaces)
	if err != nil {
		return nil, err
	}

	executionAdapter, err := newExecutionService(executions)
	if err != nil {
		return nil, err
	}

	daemonAdapter := newDaemonService(version, instanceID, shutdown)
	server := grpcgo.NewServer()
	healthServer := health.NewServer()

	daemonv1.RegisterDaemonServiceServer(server, daemonAdapter)
	workspacev1.RegisterWorkspaceServiceServer(server, workspaceAdapter)
	executionv1.RegisterExecutionServiceServer(server, executionAdapter)
	healthv1.RegisterHealthServer(server, healthServer)

	result := &Server{
		server: server,
		health: healthServer,
	}
	result.SetNotServing()

	return result, nil
}

// Serve accepts gRPC traffic until the server is stopped or the listener fails.
func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

// SetServing marks the daemon and all registered services healthy.
func (s *Server) SetServing() {
	s.setHealth(healthv1.HealthCheckResponse_SERVING)
}

// SetNotServing marks the daemon and all registered services unavailable.
func (s *Server) SetNotServing() {
	s.setHealth(healthv1.HealthCheckResponse_NOT_SERVING)
}

// Stop gracefully drains active RPCs until the context expires.
func (s *Server) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.server.Stop()
		<-done

		return ctx.Err()
	}
}

func (s *Server) setHealth(value healthv1.HealthCheckResponse_ServingStatus) {
	s.health.SetServingStatus("", value)
	s.health.SetServingStatus(daemonv1.DaemonService_ServiceDesc.ServiceName, value)
	s.health.SetServingStatus(workspacev1.WorkspaceService_ServiceDesc.ServiceName, value)
	s.health.SetServingStatus(executionv1.ExecutionService_ServiceDesc.ServiceName, value)
}
