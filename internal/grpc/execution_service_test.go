package grpc

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	executionv1 "github.com/MontFerret/ferretd/gen/ferretd/execution/v1"
	workspacev1 "github.com/MontFerret/ferretd/gen/ferretd/workspace/v1"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestExecutionServiceRequiresManager(t *testing.T) {
	service, err := newExecutionService(nil)
	if service != nil {
		t.Fatal("newExecutionService returned a service for a nil manager")
	}

	if !errors.Is(err, errNilExecutionManager) {
		t.Fatalf("newExecutionService error = %v, want %v", err, errNilExecutionManager)
	}
}

func TestExecutionServiceUsesSuppliedManager(t *testing.T) {
	workspaces := workspace.New()
	manager := mustNewExecutionManager(t, workspaces)

	service, err := newExecutionService(manager)
	if err != nil {
		t.Fatalf("newExecutionService: %v", err)
	}

	if service.executions != manager {
		t.Fatal("newExecutionService did not retain the supplied manager")
	}
}

func TestExecutionStatusErrorsCarryTypedResourceDetails(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		code      codes.Code
		resource  executionv1.ResourceKind
		condition executionv1.ResourceCondition
	}{
		{
			name:      "workspace not found",
			err:       workspace.ErrNotFound,
			code:      codes.NotFound,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		},
		{
			name:      "source not found",
			err:       workspace.ErrDocumentNotFound,
			code:      codes.NotFound,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_SOURCE,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		},
		{
			name:      "workspace closed",
			err:       workspace.ErrClosed,
			code:      codes.FailedPrecondition,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_WORKSPACE,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED,
		},
		{
			name:      "session not found",
			err:       exec.ErrSessionNotFound,
			code:      codes.NotFound,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_SESSION,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_NOT_FOUND,
		},
		{
			name:      "invalid state",
			err:       exec.ErrExecutionTerminal,
			code:      codes.FailedPrecondition,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_STATE,
		},
		{
			name:      "invalid execution options",
			err:       exec.ErrInvalidExecutionOptions,
			code:      codes.InvalidArgument,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_INVALID_OPTIONS,
		},
		{
			name:      "execution service closed",
			err:       exec.ErrClosed,
			code:      codes.FailedPrecondition,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_EXECUTION,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_CLOSED,
		},
		{
			name:      "watcher lag",
			err:       exec.ErrWatcherLagged,
			code:      codes.ResourceExhausted,
			resource:  executionv1.ResourceKind_RESOURCE_KIND_WATCHER,
			condition: executionv1.ResourceCondition_RESOURCE_CONDITION_LAGGED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := toExecutionStatusError(tt.err)

			grpcStatus := status.Convert(err)
			if grpcStatus.Code() != tt.code {
				t.Fatalf("code = %v, want %v", grpcStatus.Code(), tt.code)
			}

			var got *executionv1.ResourceErrorDetail
			for _, value := range grpcStatus.Details() {
				detail, ok := value.(*executionv1.ResourceErrorDetail)
				if ok {
					got = detail

					break
				}
			}

			if got == nil || got.Resource != tt.resource || got.Condition != tt.condition {
				t.Fatalf("resource detail = %+v, want %v/%v", got, tt.resource, tt.condition)
			}
		})
	}
}

func TestSessionAcquisitionFenceOverService(t *testing.T) {
	const key = "0ffea85f-f0a4-473d-8028-f31c98e71024"
	workspaces := workspace.New()

	opened, err := workspaces.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })

	service, err := newExecutionService(mustNewExecutionManager(t, workspaces))
	if err != nil {
		t.Fatal(err)
	}

	id := &workspacev1.WorkspaceId{Value: opened.ID().String()}
	for _, invalid := range []string{"", "bad"} {
		_, err = service.CancelSessionAcquisition(t.Context(), &executionv1.CancelSessionAcquisitionRequest{WorkspaceId: id, AcquisitionId: invalid})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid key: %v", err)
		}
	}

	_, err = service.CancelSessionAcquisition(t.Context(), &executionv1.CancelSessionAcquisitionRequest{WorkspaceId: id, AcquisitionId: key})
	if err != nil {
		t.Fatal(err)
	}

	acquisition := key

	_, err = service.CreateSession(t.Context(), &executionv1.CreateSessionRequest{WorkspaceId: id, RelativePath: "query.fql", AcquisitionId: &acquisition})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("create after cancellation: %v", err)
	}
}

func TestLostCreateReplyReconcilesPublishedSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 1"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspaces := workspace.New()
	t.Cleanup(func() { _ = workspaces.Clear(context.Background()) })

	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	native := &acquisitionRuntime{unusedRuntime: unusedRuntime{t: t}}

	manager, err := exec.New(workspaces, newTestOwner(t, native))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	service, err := newExecutionService(manager)
	if err != nil {
		t.Fatal(err)
	}

	published := make(chan *executionv1.SessionId, 1)
	server := grpcgo.NewServer(grpcgo.UnaryInterceptor(func(ctx context.Context, request any, _ *grpcgo.UnaryServerInfo, next grpcgo.UnaryHandler) (any, error) {
		response, err := next(ctx, request)
		if created, ok := response.(*executionv1.CreateSessionResponse); ok && err == nil && created.Session.AcquisitionId != "" {
			published <- created.Session.Id
			<-ctx.Done()

			return nil, ctx.Err()
		}

		return response, err
	}))
	executionv1.RegisterExecutionServiceServer(server, service)
	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpcgo.NewClient("passthrough:///bufnet", grpcgo.WithTransportCredentials(insecure.NewCredentials()),
		grpcgo.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })
	client := executionv1.NewExecutionServiceClient(conn)
	id := &workspacev1.WorkspaceId{Value: opened.ID().String()}

	sibling, err := client.CreateSession(ctx, &executionv1.CreateSessionRequest{WorkspaceId: id, RelativePath: "query.fql"})
	if err != nil {
		t.Fatal(err)
	}

	key := "35dc2f85-2392-46d5-b86e-ac8d9942c75a"
	create, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() {
		_, err := client.CreateSession(create, &executionv1.CreateSessionRequest{WorkspaceId: id, RelativePath: "query.fql", AcquisitionId: &key})
		done <- err
	}()

	var withheld *executionv1.SessionId
	select {
	case withheld = <-published:
	case err := <-done:
		t.Fatalf("create returned before publication: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	stop()

	if err := <-done; status.Code(err) != codes.Canceled {
		t.Fatalf("withheld reply: %v", err)
	}

	if _, err := client.CancelSessionAcquisition(ctx, &executionv1.CancelSessionAcquisitionRequest{WorkspaceId: id, AcquisitionId: key}); err != nil {
		t.Fatal(err)
	}

	if _, err := client.GetSession(ctx, &executionv1.GetSessionRequest{Id: withheld}); status.Code(err) != codes.NotFound {
		t.Fatalf("cancelled Session lookup: %v", err)
	}

	if _, err := client.GetSession(ctx, &executionv1.GetSessionRequest{Id: sibling.Session.Id}); err != nil {
		t.Fatalf("sibling Session lookup: %v", err)
	}

	if got := native.releases.Load(); got != 1 {
		t.Fatalf("released plans = %d, want only the withheld Session", got)
	}
}
