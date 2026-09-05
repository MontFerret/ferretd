package grpc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestWorkspaceServiceRequiresManager(t *testing.T) {
	service, err := newWorkspaceService(nil)
	if service != nil {
		t.Fatal("newWorkspaceService returned a service for a nil manager")
	}

	if !errors.Is(err, errNilWorkspaceManager) {
		t.Fatalf("newWorkspaceService error = %v, want %v", err, errNilWorkspaceManager)
	}
}

func TestWorkspaceServiceUsesSuppliedManager(t *testing.T) {
	manager := workspace.New()

	service, err := newWorkspaceService(manager)
	if err != nil {
		t.Fatalf("newWorkspaceService: %v", err)
	}

	if service.workspaces != manager {
		t.Fatal("newWorkspaceService did not retain the supplied manager")
	}
}

func TestWorkspaceServiceDelegatesLifecycle(t *testing.T) {
	manager := workspace.New()
	service := mustNewWorkspaceService(t, manager)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opened, err := service.Open(context.Background(), &workspacev1.OpenRequest{Root: root})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if opened.Workspace.Root != filepath.Clean(root) || opened.Workspace.Id.Value == "" {
		t.Fatalf("workspace = %#v", opened.Workspace)
	}

	domain, err := manager.Get(context.Background(), workspace.ID(opened.Workspace.Id.Value))
	if err != nil {
		t.Fatalf("manager Get: %v", err)
	}

	if documents := domain.Documents(); len(documents) != 1 || documents[0].Content() != "RETURN 1" {
		t.Fatalf("retained documents = %#v", documents)
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
	service := mustNewWorkspaceService(t, workspace.New())

	_, err := service.Open(context.Background(), &workspacev1.OpenRequest{Root: "relative"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Open code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestWorkspaceLoadFailureIsSanitized(t *testing.T) {
	err := toWorkspaceStatusError(fmt.Errorf("%w: private filesystem detail", workspace.ErrLoad))
	if status.Code(err) != codes.Internal {
		t.Fatalf("load code = %v, want Internal", status.Code(err))
	}

	if status.Convert(err).Message() != "workspace load failed" {
		t.Fatalf("load message = %q, want sanitized message", status.Convert(err).Message())
	}

	if strings.Contains(err.Error(), "private filesystem detail") {
		t.Fatalf("load error leaked cause: %v", err)
	}
}
