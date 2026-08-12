// Package grpc adapts daemon services to the gRPC protocol.
package grpc

import (
	"context"
	"net"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/workspace"
)

// Server owns the gRPC transport adapter and its health state.
type Server struct {
	server *grpcgo.Server
	health *health.Server
}

// New constructs the daemon's gRPC adapter.
func New(workspaces *workspace.Manager, version, instanceID string, shutdown func()) *Server {
	server := grpcgo.NewServer()
	healthServer := health.NewServer()

	daemonv1.RegisterDaemonServiceServer(server, newDaemonService(version, instanceID, shutdown))
	workspacev1.RegisterWorkspaceServiceServer(server, newWorkspaceService(workspaces))
	healthv1.RegisterHealthServer(server, healthServer)

	result := &Server{
		server: server,
		health: healthServer,
	}
	result.SetNotServing()

	return result
}

// Serve accepts gRPC traffic until the server is stopped or the listener fails.
func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

// SetServing marks the daemon and its M1 services healthy.
func (s *Server) SetServing() {
	s.setHealth(healthv1.HealthCheckResponse_SERVING)
}

// SetNotServing marks the daemon and its M1 services unavailable.
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
}
