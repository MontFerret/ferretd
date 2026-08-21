package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferret/v2"
)

func TestWorkspaceEngineIsRootedAndReadWrite(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", `
LET data = IO::FS::READ("input.txt")
IO::FS::WRITE("output.txt", data)
RETURN TO_STRING(data)
`)
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	output := runWorkspaceCompilation(t, opened, "query.fql")
	if got := string(output); got != `"inside"` {
		t.Fatalf("output = %q, want JSON string", got)
	}
	written, err := os.ReadFile(filepath.Join(root, "output.txt"))
	if err != nil {
		t.Fatalf("ReadFile output: %v", err)
	}
	if string(written) != "inside" {
		t.Fatalf("written content = %q, want inside", written)
	}

	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceSource(t, root, "escape.fql", `RETURN TO_STRING(IO::FS::READ("../outside.txt"))`)

	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	opened, err = manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	compilation, err := compileWorkspaceDocument(context.Background(), opened, "escape.fql", false)
	if err != nil {
		t.Fatalf("Compile escape: %v", err)
	}
	defer func() { _ = compilation.Close() }()

	session, err := compilation.Plan.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession escape: %v", err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Run(context.Background()); err == nil {
		t.Fatal("outside-root read unexpectedly succeeded")
	}
}

func TestWorkspaceEngineConstructionFailureDoesNotPublish(t *testing.T) {
	manager := New()
	want := errors.New("engine construction failed")
	manager.newEngine = func(string) (*ferret.Engine, error) {
		return nil, want
	}

	_, err := manager.Open(context.Background(), t.TempDir())
	if !errors.Is(err, want) || !errors.Is(err, ErrLoad) {
		t.Fatalf("Open error = %v, want engine failure and ErrLoad", err)
	}
	items, listErr := manager.List(context.Background())
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(items) != 0 {
		t.Fatalf("published workspaces = %d, want 0", len(items))
	}
}

func TestWorkspaceEngineCloseFailureIsReturned(t *testing.T) {
	manager := New()
	want := errors.New("engine close failed")
	manager.newEngine = func(root string) (*ferret.Engine, error) {
		return ferret.New(
			ferret.WithFSRoot(root),
			ferret.WithEngineCloseHook(func() error { return want }),
		)
	}

	opened, err := manager.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := manager.Close(context.Background(), opened.ID()); !errors.Is(err, want) {
		t.Fatalf("Close error = %v, want engine close failure", err)
	}
	if opened.State() != StateClosed {
		t.Fatalf("state = %v, want closed", opened.State())
	}
}

func TestWorkspaceCloseCallerCancellationDoesNotStopCleanup(t *testing.T) {
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	manager := New()
	manager.newEngine = func(root string) (*ferret.Engine, error) {
		return ferret.New(
			ferret.WithFSRoot(root),
			ferret.WithEngineCloseHook(func() error {
				close(closeStarted)
				<-releaseClose

				return nil
			}),
		)
	}

	opened, err := manager.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.Close(ctx, opened.ID()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close error = %v, want context.Canceled", err)
	}
	<-closeStarted
	if _, err := manager.Get(context.Background(), opened.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get during committed close error = %v, want ErrNotFound", err)
	}

	close(releaseClose)
	if err := manager.Close(context.Background(), opened.ID()); err != nil {
		t.Fatalf("wait for committed Close: %v", err)
	}
	if opened.State() != StateClosed {
		t.Fatalf("state = %v, want closed", opened.State())
	}
}

func TestWorkspaceClearJoinsCommittedClose(t *testing.T) {
	want := errors.New("engine close failed")
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{})
	manager := New()
	manager.newEngine = func(root string) (*ferret.Engine, error) {
		return ferret.New(
			ferret.WithFSRoot(root),
			ferret.WithEngineCloseHook(func() error {
				close(closeStarted)
				<-releaseClose

				return want
			}),
		)
	}

	opened, err := manager.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	closed := make(chan error, 1)
	go func() {
		closed <- manager.Close(context.Background(), opened.ID())
	}()
	<-closeStarted

	clearContext := newObservedDoneContext(context.Background())
	cleared := make(chan error, 1)
	go func() {
		cleared <- manager.Clear(clearContext)
	}()
	<-clearContext.observed

	close(releaseClose)
	if err := <-closed; !errors.Is(err, want) {
		t.Fatalf("Close error = %v, want %v", err, want)
	}
	if err := <-cleared; !errors.Is(err, want) {
		t.Fatalf("Clear error = %v, want %v", err, want)
	}

	manager.mu.RLock()
	entries := len(manager.byID)
	manager.mu.RUnlock()
	if entries != 0 {
		t.Fatalf("workspace entries after close = %d, want 0", entries)
	}
}

func TestWorkspaceCloseWaitsForConcurrentCompilation(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "RETURN 1")

	compileStarted := make(chan struct{})
	releaseCompile := make(chan struct{})
	closeHookStarted := make(chan struct{})
	manager := New()
	manager.RegisterCloseHook(func(context.Context, ID) error {
		close(closeHookStarted)

		return nil
	})
	manager.newEngine = func(root string) (*ferret.Engine, error) {
		return ferret.New(
			ferret.WithFSRoot(root),
			ferret.WithBeforeCompileHook(func(ctx context.Context) error {
				close(compileStarted)

				select {
				case <-releaseCompile:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}),
		)
	}

	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	compiled := make(chan Compilation, 1)
	compileErr := make(chan error, 1)
	go func() {
		result, err := compileWorkspaceDocument(context.Background(), opened, "query.fql", false)
		compiled <- result
		compileErr <- err
	}()
	<-compileStarted

	operation, owner := manager.beginClose(opened.ID())
	if operation == nil || !owner {
		t.Fatal("beginClose did not acquire workspace close ownership")
	}
	go manager.finishClose(opened.ID(), operation)
	<-closeHookStarted
	if _, err := manager.Get(context.Background(), opened.ID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get during close error = %v, want ErrNotFound", err)
	}
	close(releaseCompile)

	result := <-compiled
	if err := <-compileErr; err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Compilation.Close: %v", err)
	}
	if err := operation.close.Wait(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := opened.CompileDocument(context.Background(), Document{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Compile after close error = %v, want ErrClosed", err)
	}
}

func TestWorkspaceCompilationUsesStaticSourceSnapshot(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "RETURN 1")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	writeWorkspaceSource(t, root, "query.fql", "RETURN 2")
	compilation, err := compileWorkspaceDocument(context.Background(), opened, "query.fql", false)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer func() { _ = compilation.Close() }()

	if compilation.Source.Workspace != opened.ID() || compilation.Source.RelativePath != "query.fql" ||
		compilation.Source.Revision != 1 || compilation.Source.URI == "" {
		t.Fatalf("source snapshot = %+v", compilation.Source)
	}
	session, err := compilation.Plan.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = session.Close() }()
	output, err := session.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := string(output.Content); got != "1" {
		t.Fatalf("output = %q, want retained source result", got)
	}
}

func TestWorkspaceCompileDebugPreservesSourceSnapshotAndDebuggerMetadata(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSource(t, root, "query.fql", "LET x = 1\nRETURN x")
	manager := newTestManager(t)
	opened, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	compilation, err := compileWorkspaceDocument(context.Background(), opened, "query.fql", true)
	if err != nil {
		t.Fatalf("CompileDebug: %v", err)
	}
	defer func() { _ = compilation.Close() }()
	if compilation.Source.Workspace != opened.ID() || compilation.Source.RelativePath != "query.fql" ||
		compilation.Source.Revision != 1 || compilation.Source.URI == "" {
		t.Fatalf("source snapshot = %+v", compilation.Source)
	}

	session, err := compilation.Plan.NewDebugSession(context.Background())
	if err != nil {
		t.Fatalf("NewDebugSession: %v", err)
	}
	defer func() { _ = session.Close() }()
	event, err := session.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if event.Reason != ferret.DebugReasonEntry || event.Location.Line != 1 {
		t.Fatalf("entry event = %+v", event)
	}
}

func runWorkspaceCompilation(t *testing.T, opened *Workspace, relativePath string) []byte {
	t.Helper()

	compilation, err := compileWorkspaceDocument(context.Background(), opened, relativePath, false)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer func() { _ = compilation.Close() }()

	session, err := compilation.Plan.NewSession(context.Background())
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer func() { _ = session.Close() }()

	output, err := session.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	return append([]byte(nil), output.Content...)
}

func compileWorkspaceDocument(
	ctx context.Context,
	opened *Workspace,
	relativePath string,
	debug bool,
) (Compilation, error) {
	document, ok := opened.Document(relativePath)
	if !ok {
		return Compilation{}, ErrDocumentNotFound
	}

	if !debug {
		return opened.CompileDocument(ctx, document)
	}

	snapshot := SourceSnapshot{
		Workspace:    opened.ID(),
		RelativePath: document.File().RelativePath,
		URI:          document.File().URI,
		Revision:     document.Revision(),
	}

	return opened.CompileDebugSnapshot(ctx, snapshot, document.Content())
}

func writeWorkspaceSource(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
