package client

import (
	"context"
	"fmt"
	"path/filepath"

	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
)

// WorkspaceClient provides workspace operations over a negotiated connection.
type WorkspaceClient struct {
	client workspacev1.WorkspaceServiceClient
}

// Open ensures a workspace exists for root and returns its snapshot.
func (c *WorkspaceClient) Open(ctx context.Context, root string) (Workspace, error) {
	if root == "" {
		return Workspace{}, ErrInvalidWorkspaceRoot
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: resolve absolute root: %v", ErrInvalidWorkspaceRoot, err)
	}

	response, err := c.client.Open(ctx, &workspacev1.OpenRequest{Root: filepath.Clean(absolute)})
	if err != nil {
		return Workspace{}, mapError(ctx, err)
	}

	if response == nil {
		return Workspace{}, errIncompleteWorkspace
	}

	return fromProtoWorkspace(response.Workspace)
}

// Get returns a workspace by ID.
func (c *WorkspaceClient) Get(ctx context.Context, id string) (Workspace, error) {
	response, err := c.client.Get(ctx, &workspacev1.GetRequest{
		Id: &workspacev1.WorkspaceId{Value: id},
	})
	if err != nil {
		return Workspace{}, mapError(ctx, err)
	}

	if response == nil {
		return Workspace{}, errIncompleteWorkspace
	}

	return fromProtoWorkspace(response.Workspace)
}

// List returns workspaces ordered by root.
func (c *WorkspaceClient) List(ctx context.Context) ([]Workspace, error) {
	response, err := c.client.List(ctx, &workspacev1.ListRequest{})
	if err != nil {
		return nil, mapError(ctx, err)
	}

	if response == nil {
		return nil, errIncompleteWorkspace
	}

	result := make([]Workspace, 0, len(response.Workspaces))
	for _, item := range response.Workspaces {
		workspace, err := fromProtoWorkspace(item)
		if err != nil {
			return nil, err
		}

		result = append(result, workspace)
	}

	return result, nil
}

// Close removes a workspace. Closing an unknown workspace is safe.
func (c *WorkspaceClient) Close(ctx context.Context, id string) error {
	_, err := c.client.Close(ctx, &workspacev1.CloseRequest{
		Id: &workspacev1.WorkspaceId{Value: id},
	})

	return mapError(ctx, err)
}
