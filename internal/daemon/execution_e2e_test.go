package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	supportedclient "github.com/MontFerret/ferretd/client"
)

func TestSupportedClientExecutionWorkingDirectoryOutsideWorkspace(t *testing.T) {
	endpoint := testEndpoint(t)
	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	startDone := make(chan error, 1)
	go func() { startDone <- d.Start(context.Background()) }()
	waitForEndpoint(t, endpoint)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = d.Stop(ctx)
		<-startDone
	})

	workspaceRoot := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(workspaceRoot, "query.fql"),
		[]byte(`RETURN TO_STRING(IO::FS::READ("value.txt"))`),
		0o600,
	); err != nil {
		t.Fatalf("write query: %v", err)
	}
	runtimeRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(runtimeRoot, "value.txt"), []byte("runtime"), 0o600); err != nil {
		t.Fatalf("write runtime value: %v", err)
	}
	canonicalRuntimeRoot, err := filepath.EvalSymlinks(runtimeRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	publicEndpoint, err := supportedclient.ParseEndpoint(endpoint.String())
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := supportedclient.Dial(ctx, supportedclient.WithEndpoint(publicEndpoint))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	workspace, err := client.Workspaces().Open(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("Workspace Open: %v", err)
	}
	session, err := client.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{
		WorkspaceID:  workspace.ID,
		RelativePath: "query.fql",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	created, err := client.Executions().CreateExecution(ctx, supportedclient.CreateExecutionRequest{
		SessionID: session.ID,
		Options: supportedclient.ExecutionOptions{
			WorkingDirectory: runtimeRoot,
		},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if created.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
		t.Fatalf("WorkingDirectory = %q, want %q", created.Options.WorkingDirectory, canonicalRuntimeRoot)
	}

	watch, err := client.Executions().WatchExecution(ctx, created.ID)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	if event, err := watch.Recv(); err != nil || event.Kind != supportedclient.ExecutionEventCreated ||
		event.Execution.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
		t.Fatalf("created event = %+v, %v", event, err)
	}
	running, err := client.Executions().RunExecution(ctx, created.ID)
	if err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if running.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
		t.Fatalf("running WorkingDirectory = %q, want %q", running.Options.WorkingDirectory, canonicalRuntimeRoot)
	}
	if event, err := watch.Recv(); err != nil || event.Kind != supportedclient.ExecutionEventStarted ||
		event.Execution.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
		t.Fatalf("started event = %+v, %v", event, err)
	}
	terminal, err := watch.Recv()
	if err != nil {
		t.Fatalf("terminal event: %v", err)
	}
	if terminal.Kind != supportedclient.ExecutionEventCompleted || terminal.Execution.Output == nil ||
		string(terminal.Execution.Output.Data) != `"runtime"` ||
		terminal.Execution.Options.WorkingDirectory != filepath.Clean(canonicalRuntimeRoot) {
		t.Fatalf("terminal event = %+v", terminal)
	}
}

func TestSupportedClientExecutionFlowAndReconnectPersistence(t *testing.T) {
	endpoint := testEndpoint(t)
	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	startDone := make(chan error, 1)
	go func() { startDone <- d.Start(context.Background()) }()
	waitForEndpoint(t, endpoint)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = d.Stop(ctx)
		<-startDone
	})

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN @value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid.fql"), []byte("RETURN missing"), 0o600); err != nil {
		t.Fatal(err)
	}

	publicEndpoint, err := supportedclient.ParseEndpoint(endpoint.String())
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first, err := supportedclient.Dial(ctx, supportedclient.WithEndpoint(publicEndpoint))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	workspace, err := first.Workspaces().Open(ctx, root)
	if err != nil {
		t.Fatalf("Workspace Open: %v", err)
	}
	if _, err := first.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{
		WorkspaceID:  "missing",
		RelativePath: "query.fql",
	}); !errors.Is(err, supportedclient.ErrWorkspaceNotFound) {
		t.Fatalf("missing workspace error = %v, want ErrWorkspaceNotFound", err)
	}
	if _, err := first.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{
		WorkspaceID:  workspace.ID,
		RelativePath: "missing.fql",
	}); !errors.Is(err, supportedclient.ErrExecutionSourceNotFound) {
		t.Fatalf("missing source error = %v, want ErrExecutionSourceNotFound", err)
	}
	if err := os.WriteFile(filepath.Join(root, "created-after-open.fql"), []byte("RETURN 7"), 0o600); err != nil {
		t.Fatal(err)
	}
	dynamicSession, err := first.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{
		WorkspaceID:  workspace.ID,
		RelativePath: "created-after-open.fql",
	})
	if err != nil {
		t.Fatalf("dynamic CreateSession: %v", err)
	}
	dynamicExecution, err := first.Executions().CreateExecution(ctx, supportedclient.CreateExecutionRequest{
		SessionID: dynamicSession.ID,
	})
	if err != nil {
		t.Fatalf("dynamic CreateExecution: %v", err)
	}
	dynamicWatch, err := first.Executions().WatchExecution(ctx, dynamicExecution.ID)
	if err != nil {
		t.Fatalf("dynamic WatchExecution: %v", err)
	}
	if _, err := dynamicWatch.Recv(); err != nil {
		t.Fatalf("dynamic current Recv: %v", err)
	}
	if _, err := first.Executions().RunExecution(ctx, dynamicExecution.ID); err != nil {
		t.Fatalf("dynamic RunExecution: %v", err)
	}
	if _, err := dynamicWatch.Recv(); err != nil {
		t.Fatalf("dynamic started Recv: %v", err)
	}
	dynamicTerminal, err := dynamicWatch.Recv()
	if err != nil {
		t.Fatalf("dynamic terminal Recv: %v", err)
	}
	if dynamicTerminal.Kind != supportedclient.ExecutionEventCompleted || dynamicTerminal.Execution.Output == nil ||
		string(dynamicTerminal.Execution.Output.Data) != "7" {
		t.Fatalf("dynamic terminal event = %+v", dynamicTerminal)
	}
	if err := first.Executions().CloseExecution(ctx, dynamicExecution.ID); err != nil {
		t.Fatalf("dynamic CloseExecution: %v", err)
	}
	if err := first.Executions().CloseSession(ctx, dynamicSession.ID); err != nil {
		t.Fatalf("dynamic CloseSession: %v", err)
	}
	session, err := first.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{
		WorkspaceID:  workspace.ID,
		RelativePath: "query.fql",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.Source.WorkspaceID != workspace.ID || session.Source.RelativePath != "query.fql" ||
		session.Source.URI == "" || session.Source.Revision != 1 {
		t.Fatalf("Session = %+v", session)
	}

	_, err = first.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{
		WorkspaceID:  workspace.ID,
		RelativePath: "invalid.fql",
	})
	if !errors.Is(err, supportedclient.ErrCompilationFailed) {
		t.Fatalf("invalid CreateSession error = %v, want ErrCompilationFailed", err)
	}
	var compilation *supportedclient.CompilationError
	if !errors.As(err, &compilation) || len(compilation.Diagnostics) == 0 ||
		compilation.Source.RelativePath != "invalid.fql" {
		t.Fatalf("CompilationError = %+v", compilation)
	}

	created, err := first.Executions().CreateExecution(ctx, supportedclient.CreateExecutionRequest{
		SessionID:  session.ID,
		Parameters: map[string]any{"value": 42},
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if created.State != supportedclient.ExecutionStateCreated ||
		created.Options.OutputContentType != "application/json" {
		t.Fatalf("created = %+v", created)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first client Close: %v", err)
	}

	second, err := supportedclient.Dial(ctx, supportedclient.WithEndpoint(publicEndpoint))
	if err != nil {
		t.Fatalf("reconnect Dial: %v", err)
	}
	defer func() { _ = second.Close() }()
	if _, err := second.Executions().GetSession(ctx, session.ID); err != nil {
		t.Fatalf("GetSession after reconnect: %v", err)
	}
	if _, err := second.Executions().GetExecution(ctx, created.ID); err != nil {
		t.Fatalf("GetExecution after reconnect: %v", err)
	}

	watcher, err := second.Executions().WatchExecution(ctx, created.ID)
	if err != nil {
		t.Fatalf("WatchExecution: %v", err)
	}
	current, err := watcher.Recv()
	if err != nil {
		t.Fatalf("current Recv: %v", err)
	}
	if current.Kind != supportedclient.ExecutionEventCreated || current.Sequence != 1 ||
		current.ExecutionID != created.ID {
		t.Fatalf("current event = %+v", current)
	}

	running, err := second.Executions().RunExecution(ctx, created.ID)
	if err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if running.State != supportedclient.ExecutionStateRunning {
		t.Fatalf("RunExecution state = %v, want running", running.State)
	}
	started, err := watcher.Recv()
	if err != nil {
		t.Fatalf("started Recv: %v", err)
	}
	terminal, err := watcher.Recv()
	if err != nil {
		t.Fatalf("terminal Recv: %v", err)
	}
	if started.Kind != supportedclient.ExecutionEventStarted || started.Sequence != 2 ||
		terminal.Kind != supportedclient.ExecutionEventCompleted || terminal.Sequence != 3 ||
		terminal.Execution.Output == nil || string(terminal.Execution.Output.Data) != "42" {
		t.Fatalf("events: started=%+v terminal=%+v", started, terminal)
	}
	if _, err := watcher.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal Recv error = %v, want io.EOF", err)
	}

	retained, err := second.Executions().GetExecution(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetExecution terminal: %v", err)
	}
	if retained.State != supportedclient.ExecutionStateCompleted || retained.Output == nil ||
		string(retained.Output.Data) != "42" {
		t.Fatalf("retained = %+v", retained)
	}
	if err := second.Executions().CloseExecution(ctx, created.ID); err != nil {
		t.Fatalf("CloseExecution: %v", err)
	}
	if _, err := second.Executions().GetExecution(ctx, created.ID); !errors.Is(err, supportedclient.ErrExecutionNotFound) {
		t.Fatalf("GetExecution closed error = %v, want ErrExecutionNotFound", err)
	}

	disconnected, err := second.Executions().CreateExecution(ctx, supportedclient.CreateExecutionRequest{
		SessionID: session.ID,
	})
	if err != nil {
		t.Fatalf("CreateExecution for disconnect: %v", err)
	}
	watchCtx, cancelWatch := context.WithCancel(ctx)
	disconnectedWatcher, err := second.Executions().WatchExecution(watchCtx, disconnected.ID)
	if err != nil {
		t.Fatalf("WatchExecution for disconnect: %v", err)
	}
	if _, err := disconnectedWatcher.Recv(); err != nil {
		t.Fatalf("disconnect current Recv: %v", err)
	}
	cancelWatch()
	if _, err := disconnectedWatcher.Recv(); !errors.Is(err, context.Canceled) {
		t.Fatalf("disconnected Recv error = %v, want context.Canceled", err)
	}
	stillCreated, err := second.Executions().GetExecution(ctx, disconnected.ID)
	if err != nil {
		t.Fatalf("GetExecution after watcher disconnect: %v", err)
	}
	if stillCreated.State != supportedclient.ExecutionStateCreated {
		t.Fatalf("state after watcher disconnect = %v, want created", stillCreated.State)
	}
	cancelled, err := second.Executions().CancelExecution(ctx, disconnected.ID)
	if err != nil {
		t.Fatalf("CancelExecution: %v", err)
	}
	if cancelled.State != supportedclient.ExecutionStateCancelled {
		t.Fatalf("CancelExecution state = %v, want cancelled", cancelled.State)
	}
	if _, err := second.Executions().RunExecution(ctx, disconnected.ID); !errors.Is(err, supportedclient.ErrInvalidExecutionState) {
		t.Fatalf("RunExecution cancelled error = %v, want ErrInvalidExecutionState", err)
	}
	if err := second.Executions().CloseExecution(ctx, disconnected.ID); err != nil {
		t.Fatalf("CloseExecution cancelled: %v", err)
	}
	if err := second.Executions().CloseSession(ctx, session.ID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if err := second.Workspaces().Close(ctx, workspace.ID); err != nil {
		t.Fatalf("Workspace Close: %v", err)
	}
}
