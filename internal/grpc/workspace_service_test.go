package grpc

import (
	"context"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestWorkspaceServiceDelegatesLifecycle(t *testing.T) {
	manager := workspace.New()
	service := newWorkspaceService(manager)
	root := t.TempDir()

	opened, err := service.Open(context.Background(), &workspacev1.OpenRequest{Root: root})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Workspace.Root != filepath.Clean(root) || opened.Workspace.Id.Value == "" {
		t.Fatalf("workspace = %#v", opened.Workspace)
	}

	got, err := service.Get(context.Background(), &workspacev1.GetRequest{Id: opened.Workspace.Id})
	if err != nil || got.Workspace.Id.Value != opened.Workspace.Id.Value {
		t.Fatalf("Get = %#v, %v", got, err)
	}

	listed, err := service.List(context.Background(), &workspacev1.ListRequest{})
	if err != nil || len(listed.Workspaces) != 1 {
		t.Fatalf("List = %#v, %v", listed, err)
	}

	if _, err := service.Close(context.Background(), &workspacev1.CloseRequest{Id: opened.Workspace.Id}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := service.Close(context.Background(), &workspacev1.CloseRequest{Id: opened.Workspace.Id}); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}

	_, err = service.Get(context.Background(), &workspacev1.GetRequest{Id: opened.Workspace.Id})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("Get closed code = %v, want NotFound", status.Code(err))
	}
}

func TestWorkspaceServiceMapsInvalidRoot(t *testing.T) {
	service := newWorkspaceService(workspace.New())

	_, err := service.Open(context.Background(), &workspacev1.OpenRequest{Root: "relative"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Open code = %v, want InvalidArgument", status.Code(err))
	}
}
