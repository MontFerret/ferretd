package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/MontFerret/ferretd/client"
	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
	wirev1 "github.com/MontFerret/wire/gen/ferret/wire/v1"
	wireserver "github.com/MontFerret/wire/server"
)

func TestSessionRuntimeThroughSupportedClient(t *testing.T) {
	wire := newWireFixture(t, 41)
	f := newDaemonFixture(t, runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN TASK3_APPLICATION_VALUE() + @offset")
	writeSourceFile(t, root, "plain.fql", "RETURN 7")

	opened, err := f.connection.Workspaces().Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	c := f.connection.Executions()

	created, err := c.CreateSession(ctx, client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql", RuntimeName: "application"})
	if err != nil || created.RuntimeName != "application" || len(created.Parameters) != 1 {
		t.Fatalf("CreateSession = %+v, %v", created, err)
	}

	fetched, err := c.GetSession(ctx, created.ID)
	if err != nil || fetched.RuntimeName != "application" {
		t.Fatalf("GetSession = %+v, %v", fetched, err)
	}

	for range 2 {
		if got := runPublicSession(ctx, t, c, created.ID); got != "42" {
			t.Fatalf("output = %q", got)
		}
	}

	var runs sync.WaitGroup
	for range 4 {
		runs.Go(func() {
			if got := runPublicSession(ctx, t, c, created.ID); got != "42" {
				t.Errorf("concurrent output = %q", got)
			}
		})
	}

	runs.Wait()

	if wire.acquisitions.Load() != 1 {
		t.Fatalf("acquisitions = %d", wire.acquisitions.Load())
	}

	second, err := c.CreateSession(ctx, client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql", RuntimeName: "application"})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.CloseSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if got := runPublicSession(ctx, t, c, second.ID); got != "42" || wire.acquisitions.Load() != 2 {
		t.Fatalf("independent Session = %q, acquisitions %d", got, wire.acquisitions.Load())
	}

	for _, selector := range []string{"", "local"} {
		local, err := c.CreateSession(ctx, client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "plain.fql", RuntimeName: selector})
		if err != nil || local.RuntimeName != "local" {
			t.Fatalf("native = %+v, %v", local, err)
		}
	}

	native, err := c.CreateSession(ctx, client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql", RuntimeName: "local"})
	if err != nil {
		t.Fatal(err)
	}

	nativeRun, err := c.CreateExecution(ctx, client.CreateExecutionRequest{SessionID: native.ID, Parameters: map[string]any{"offset": 1}})
	if err != nil {
		t.Fatal(err)
	}

	watch, err := c.WatchExecution(ctx, nativeRun.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.RunExecution(ctx, nativeRun.ID); err != nil {
		t.Fatal(err)
	}

	for {
		event, err := watch.Recv()
		if err != nil {
			t.Fatal(err)
		}

		if event.Execution.State == client.ExecutionStateFailed {
			break
		}

		if event.Execution.State == client.ExecutionStateCompleted {
			t.Fatal("native execution used an application function")
		}
	}

	for _, selector := range []string{"unknown", " application ", wire.endpoint} {
		_, err = c.CreateSession(ctx, client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "plain.fql", RuntimeName: selector})
		if !errors.Is(err, client.ErrRuntimeNotFound) || errors.Is(err, client.ErrExecutionSourceNotFound) {
			t.Fatalf("unknown selection = %v", err)
		}
	}

	if wire.acquisitions.Load() != 2 {
		t.Fatal("native or unknown selection connected remotely")
	}
}

func runPublicSession(ctx context.Context, t *testing.T, c *client.ExecutionClient, id client.SessionID) string {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	created, err := c.CreateExecution(ctx, client.CreateExecutionRequest{SessionID: id, Parameters: map[string]any{"offset": 1}})
	if err != nil {
		t.Error(err)

		return ""
	}

	defer func() {
		if err := c.CloseExecution(ctx, created.ID); err != nil {
			t.Error(err)
		}
	}()

	watch, err := c.WatchExecution(ctx, created.ID)
	if err != nil {
		t.Error(err)

		return ""
	}

	if _, err := c.RunExecution(ctx, created.ID); err != nil {
		t.Error(err)

		return ""
	}

	for {
		event, err := watch.Recv()
		if err != nil {
			t.Error(err)

			return ""
		}

		switch event.Execution.State {
		case client.ExecutionStateCompleted:
			if event.Execution.Output == nil {
				t.Error("missing output")

				return ""
			}

			return string(event.Execution.Output.Data)
		case client.ExecutionStateFailed, client.ExecutionStateCancelled:
			t.Errorf("execution failed: %+v", event.Execution.Failure)

			return ""
		}
	}
}

func TestWireDefaultIsLazyAndRuntimeFailuresAreClassified(t *testing.T) {
	var attempts atomic.Int32
	wire := newWireFixture(t, 41, wireserver.WithStreamInterceptors(func(host any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		if strings.HasSuffix(info.FullMethod, "/Connect") {
			attempts.Add(1)

			return status.Error(codes.Unavailable, "application offline")
		}

		return next(host, stream)
	}))
	f := newDaemonFixture(t, runtimes.Config{DefaultRuntime: new("application"), Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})

	catalog, err := f.connection.ListRuntimes(t.Context())
	if err != nil || catalog.DefaultRuntime != "application" || attempts.Load() != 0 {
		t.Fatalf("discovery = %+v, %v, attempts %d", catalog, err, attempts.Load())
	}

	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN 1")

	opened, err := f.connection.Workspaces().Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.connection.Executions().CreateSession(t.Context(), client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "missing.fql"})
	if !errors.Is(err, client.ErrExecutionSourceNotFound) || attempts.Load() != 0 {
		t.Fatalf("source failure = %v, attempts %d", err, attempts.Load())
	}

	_, err = f.connection.Executions().CreateSession(t.Context(), client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql"})
	if !errors.Is(err, client.ErrRuntimeUnavailable) || errors.Is(err, client.ErrDaemonUnavailable) || attempts.Load() != 1 {
		t.Fatalf("acquisition failure = %v, attempts %d", err, attempts.Load())
	}

	if _, err := f.connection.Info(t.Context()); err != nil {
		t.Fatalf("daemon unavailable after runtime failure: %v", err)
	}
}

func TestBoundWireSessionDebugSourceAndFilesystemOptions(t *testing.T) {
	var normalCompiles, debugCompiles atomic.Int32
	roots := make(chan *string, 8)
	wire := newWireFixture(t, 41, wireserver.WithUnaryInterceptors(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		var root *string
		switch value := request.(type) {
		case *wirev1.CompileRequest:
			normalCompiles.Add(1)
		case *wirev1.CompileDebugRequest:
			debugCompiles.Add(1)

			if value.Source.Content != "RETURN TASK3_APPLICATION_VALUE()" {
				return nil, status.Error(codes.Internal, "debug source changed")
			}
		case *wirev1.CreateSessionRequest:
			root = value.FsRoot
			roots <- root
		case *wirev1.CreateDebugSessionRequest:
			root = value.FsRoot
			roots <- root
		}

		if root != nil && *root == `  X:\application\data  ` {
			return nil, status.Error(codes.InvalidArgument, "host rejected root")
		}

		return next(ctx, request)
	}))
	owner := newWireOwner(t, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true})
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN TASK3_APPLICATION_VALUE()")
	workspaces := workspace.New()

	opened, err := workspaces.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := exec.New(workspaces, owner)
	if err != nil {
		t.Fatal(err)
	}

	debugs, err := debug.New(manager)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()); _ = workspaces.Clear(context.Background()) })
	creationCtx, cancel := context.WithCancel(t.Context())
	compiled, err := manager.CreateSession(creationCtx, opened.ID(), "query.fql", "application")
	cancel()

	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 999"), 0600); err != nil {
		t.Fatal(err)
	}

	if got := runSessionOutput(t, manager, compiled.ID); got != "41" {
		t.Fatalf("normal output = %q", got)
	}

	if fsRoot := <-roots; fsRoot != nil {
		t.Fatalf("implicit remote root = %q", *fsRoot)
	}

	created, err := debugs.CreateSession(t.Context(), compiled.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if fsRoot := <-roots; fsRoot != nil {
		t.Fatalf("implicit debug root = %q", *fsRoot)
	}

	subscription, err := debugs.WatchSession(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()

	if _, err := debugs.StartSession(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}

	ctx, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	completed := false
	for !completed {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				t.Fatal("debug watch closed before completion")
			}

			switch event.Snapshot.State {
			case debug.StateStopped:
				if _, err := debugs.ContinueSession(ctx, created.ID); err != nil {
					t.Fatal(err)
				}
			case debug.StateCompleted:
				if event.Snapshot.Output == nil || string(event.Snapshot.Output.Data) != "41" {
					t.Fatalf("debug output = %+v", event.Snapshot.Output)
				}

				completed = true
			case debug.StateFailed, debug.StateTerminated:
				t.Fatalf("debug failed: %+v", event.Snapshot)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

	if err := debugs.CloseSession(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	const hostRoot = `  X:\application\data  `

	ordinary, err := manager.CreateExecution(ctx, compiled.ID, nil, exec.RuntimeOptions{WorkingDirectory: hostRoot})
	if err != nil || ordinary.Options.WorkingDirectory != hostRoot {
		t.Fatalf("host path preparation = %+v, %v", ordinary, err)
	}

	terminal, _ := runAndObserve(t, manager, ordinary.ID)
	if terminal.State != exec.StateFailed || terminal.Failure.Category != exec.FailureSessionCreation {
		t.Fatalf("remote validation = %+v", terminal)
	}

	if got := <-roots; got == nil || *got != hostRoot {
		t.Fatalf("forwarded root = %v", got)
	}

	_, err = manager.CreateDebugRuntime(ctx, compiled.ID, nil, exec.RuntimeOptions{WorkingDirectory: hostRoot})
	if err == nil {
		t.Fatal("host validation was ignored")
	}

	if got := <-roots; got == nil || *got != hostRoot {
		t.Fatalf("forwarded debug root = %v", got)
	}

	if wire.acquisitions.Load() != 1 || normalCompiles.Load() != 1 || debugCompiles.Load() != 1 {
		t.Fatalf("reuse counts = %d/%d/%d", wire.acquisitions.Load(), normalCompiles.Load(), debugCompiles.Load())
	}

	writeSourceFile(t, root, "filesystem.fql", `RETURN TO_STRING(IO::FS::READ("value.txt"))`)
	writeSourceFile(t, root, "value.txt", "daemon workspace")
	writeSourceFile(t, wire.root, "value.txt", "hosted root")

	filesystem, err := manager.CreateSession(ctx, opened.ID(), "filesystem.fql", "application")
	if err != nil {
		t.Fatal(err)
	}

	if got := runSessionOutput(t, manager, filesystem.ID); got != `"hosted root"` {
		t.Fatalf("hosted default root output = %q", got)
	}
}

func TestWorkspaceCloseCancelsWireAcquisition(t *testing.T) {
	started := make(chan struct{})
	wire := newWireFixture(t, 41, wireserver.WithStreamInterceptors(func(host any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		if strings.HasSuffix(info.FullMethod, "/Connect") {
			close(started)
			<-stream.Context().Done()

			return stream.Context().Err()
		}

		return next(host, stream)
	}))
	owner := newWireOwner(t, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true})
	workspaces := workspace.New()
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN 1")

	opened, err := workspaces.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := exec.New(workspaces, owner)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = manager.Close(context.Background()); _ = workspaces.Clear(context.Background()) })
	result := make(chan error, 1)
	go func() {
		_, err := manager.CreateSession(t.Context(), opened.ID(), "query.fql", "application")
		result <- err
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if err := workspaces.Close(ctx, opened.ID()); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, workspace.ErrClosed) {
			t.Fatalf("acquisition cancellation = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("acquisition did not roll back")
	}
}

func TestWireSessionCloseReleasesPlansBeforeBinding(t *testing.T) {
	for _, failClose := range []bool{false, true} {
		name := "success"

		if failClose {
			name = "plan close failure"
		}

		t.Run(name, func(t *testing.T) {
			events := make(chan string, 16)
			wire := newWireFixture(t, 41,
				wireserver.WithUnaryInterceptors(func(ctx context.Context, request any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
					response, err := next(ctx, request)

					if strings.HasSuffix(info.FullMethod, "/ClosePlan") {
						events <- "plan"

						if failClose {
							return nil, status.Error(codes.Internal, "hosted plan close failed")
						}
					}

					return response, err
				}),
				wireserver.WithStreamInterceptors(func(host any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
					if strings.HasSuffix(info.FullMethod, "/Connect") {
						defer func() { events <- "binding" }()
					}

					return next(host, stream)
				}),
			)
			owner := newWireOwner(t, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true})
			workspaces := workspace.New()
			root := t.TempDir()
			writeSourceFile(t, root, "query.fql", "RETURN TASK3_APPLICATION_VALUE()")

			opened, err := workspaces.Open(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}

			manager, err := exec.New(workspaces, owner)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = manager.Close(context.Background()); _ = workspaces.Clear(context.Background()) })

			compiled, err := manager.CreateSession(t.Context(), opened.ID(), "query.fql", "application")
			if err != nil {
				t.Fatal(err)
			}

			debugger, err := manager.CreateDebugRuntime(t.Context(), compiled.ID, nil, exec.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}

			manager.RegisterSessionCloseHook(func(context.Context, exec.SessionID) error { events <- "child"; return debugger.Close() })

			closeErr := manager.CloseSession(t.Context(), compiled.ID)
			if failClose {
				if closeErr == nil || !strings.Contains(closeErr.Error(), "hosted plan close failed") {
					t.Fatalf("Session close = %v", closeErr)
				}
			} else if closeErr != nil {
				t.Fatal(closeErr)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			for _, want := range []string{"child", "plan", "plan", "binding"} {
				select {
				case got := <-events:
					if got != want {
						t.Fatalf("cleanup step = %s, want %s", got, want)
					}
				case <-ctx.Done():
					t.Fatal("cleanup did not finish")
				}
			}
		})
	}
}

func TestNewSessionAcquiresAfterWireDisconnect(t *testing.T) {
	wire := newWireFixture(t, 41)
	f := newDaemonFixture(t, runtimes.Config{DefaultRuntime: new("application"), Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN TASK3_APPLICATION_VALUE()+@offset")

	opened, err := f.connection.Workspaces().Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	c := f.connection.Executions()

	first, err := c.CreateSession(t.Context(), client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql"})
	if err != nil || first.RuntimeName != "application" {
		t.Fatalf("default = %+v, %v", first, err)
	}

	// The deliberately disconnected binding may retain a cleanup failure.
	f.allowStopFailure = true
	wire.closeServer(t)

	invocation, err := c.CreateExecution(t.Context(), client.CreateExecutionRequest{SessionID: first.ID})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.RunExecution(t.Context(), invocation.ID); err != nil {
		t.Fatal(err)
	}

	watch, err := c.WatchExecution(t.Context(), invocation.ID)
	if err != nil {
		t.Fatal(err)
	}

	for {
		event, err := watch.Recv()
		if err != nil {
			t.Fatal(err)
		}

		if event.Execution.State == client.ExecutionStateFailed {
			break
		}

		if event.Execution.State == client.ExecutionStateCompleted {
			t.Fatal("disconnected Session succeeded")
		}
	}

	if wire.acquisitions.Load() != 1 {
		t.Fatal("disconnected Session was reacquired")
	}

	replacement := newWireFixtureAt(t, 99, strings.TrimPrefix(wire.endpoint, "tcp://"))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	second, err := c.CreateSession(ctx, client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql"})
	if err != nil {
		t.Fatal(err)
	}

	if got := runPublicSession(ctx, t, c, second.ID); got != "100" || replacement.acquisitions.Load() != 1 {
		t.Fatalf("replacement result = %q, acquisitions %d", got, replacement.acquisitions.Load())
	}

	retained, err := c.GetSession(ctx, first.ID)
	if err != nil || retained.RuntimeName != "application" {
		t.Fatalf("original Session = %+v, %v", retained, err)
	}
}

func TestWireCompilationFailurePreservesReadableCause(t *testing.T) {
	wire := newWireFixture(t, 41, wireserver.WithUnaryInterceptors(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if _, ok := request.(*wirev1.CompileRequest); ok {
			return nil, status.Error(codes.Unavailable, "application compiler unavailable")
		}

		return next(ctx, request)
	}))
	f := newDaemonFixture(t, runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})
	root := t.TempDir()
	writeSourceFile(t, root, "query.fql", "RETURN 1")

	opened, err := f.connection.Workspaces().Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.connection.Executions().CreateSession(t.Context(), client.CreateSessionRequest{WorkspaceID: opened.ID, RelativePath: "query.fql", RuntimeName: "application"})

	var compilation *client.CompilationError
	if !errors.As(err, &compilation) || !errors.Is(err, client.ErrCompilationFailed) || errors.Is(err, client.ErrRuntimeUnavailable) || errors.Is(err, client.ErrDaemonUnavailable) || !strings.Contains(err.Error(), "application compiler unavailable") || len(compilation.Diagnostics) != 0 {
		t.Fatalf("compilation error = %v", err)
	}
}
