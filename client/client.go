package client

import (
	"context"
	"errors"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/transport"
)

var currentAPIVersion = APIVersion{Major: 1, Minor: 0}

// Client owns a negotiated connection to a local daemon.
type Client struct {
	connection *grpc.ClientConn
	daemon     daemonv1.DaemonServiceClient
	workspaces *WorkspaceClient
}

// Dial discovers or selects an endpoint, connects, and negotiates API compatibility.
func Dial(ctx context.Context, options ...Option) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	configuration := dialOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(&configuration); err != nil {
			return nil, err
		}
	}

	endpoint, err := configuredEndpoint(configuration)
	if err != nil {
		return nil, err
	}

	transportEndpoint := endpoint.transportEndpoint()
	connection, err := grpc.NewClient(
		"passthrough:///ferretd",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return transport.Dial(ctx, transportEndpoint)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create daemon client: %w", err)
	}

	result := &Client{
		connection: connection,
		daemon:     daemonv1.NewDaemonServiceClient(connection),
	}
	result.workspaces = &WorkspaceClient{
		client: workspacev1.NewWorkspaceServiceClient(connection),
	}

	if _, err := result.Info(ctx); err != nil {
		_ = result.Close()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.Join(ErrDaemonUnavailable, err)
		}

		return nil, err
	}

	return result, nil
}

// Info returns daemon identity and version information.
func (c *Client) Info(ctx context.Context) (ServerInfo, error) {
	response, err := c.daemon.GetInfo(ctx, &daemonv1.GetInfoRequest{
		ClientApi: toProtoAPIVersion(currentAPIVersion),
	})
	if err != nil {
		return ServerInfo{}, mapError(ctx, err)
	}

	if response == nil || response.ServerInfo == nil || response.ServerInfo.ApiVersion == nil {
		return ServerInfo{}, errIncompleteServerInfo
	}

	return ServerInfo{
		Version:    response.ServerInfo.Version,
		InstanceID: response.ServerInfo.InstanceId,
		APIVersion: fromProtoAPIVersion(response.ServerInfo.ApiVersion),
	}, nil
}

// Shutdown requests an idempotent graceful daemon shutdown.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.daemon.Shutdown(ctx, &daemonv1.ShutdownRequest{})
	return mapError(ctx, err)
}

// Workspaces returns the workspace operations for this connection.
func (c *Client) Workspaces() *WorkspaceClient {
	return c.workspaces
}

// Close releases the client connection without changing daemon state.
func (c *Client) Close() error {
	return c.connection.Close()
}
