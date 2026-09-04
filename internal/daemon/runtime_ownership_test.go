package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/internal/transport"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestDaemonClosesChildrenBeforeOwnedRuntimeExactlyOnce(t *testing.T) {
	runtime := newRuntimeLifecycleSpy()
	options, err := (Options{Endpoint: testEndpoint(t)}).normalized()
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	daemon, err := newDaemon(options, runtime)
	if err != nil {
		t.Fatalf("newDaemon: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opened, err := daemon.workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := daemon.executions.CreateSession(context.Background(), opened.ID(), "query.fql"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	daemon.workspaces.RegisterCloseHook(func(context.Context, workspace.ID) error {
		runtime.record("workspace")

		return nil
	})

	for range 2 {
		if err := daemon.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}

	if got, want := runtime.recordedEvents(), []string{"plan", "workspace", "runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup events = %v, want %v", got, want)
	}
	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("runtime close calls = %d, want 1", runtime.closeCalls.Load())
	}
}

func TestDaemonStopCancellationDoesNotAbandonRuntimeCleanup(t *testing.T) {
	release := make(chan struct{})
	runtime := newRuntimeLifecycleSpy()
	runtime.releaseClose = release
	options, err := (Options{Endpoint: testEndpoint(t)}).normalized()
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	daemon, err := newDaemon(options, runtime)
	if err != nil {
		t.Fatalf("newDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := daemon.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop error = %v, want context.Canceled", err)
	}

	select {
	case <-runtime.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("committed cleanup did not reach runtime close")
	}
	close(release)

	if err := daemon.Stop(context.Background()); err != nil {
		t.Fatalf("wait for Stop: %v", err)
	}
	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("runtime close calls = %d, want 1", runtime.closeCalls.Load())
	}
}

func TestDaemonStartupFailureClosesOwnedRuntime(t *testing.T) {
	endpoint := testEndpoint(t)
	listener, err := transport.Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	runtime := newRuntimeLifecycleSpy()
	options, err := (Options{Endpoint: endpoint}).normalized()
	if err != nil {
		t.Fatalf("normalize options: %v", err)
	}
	daemon, err := newDaemon(options, runtime)
	if err != nil {
		t.Fatalf("newDaemon: %v", err)
	}

	if err := daemon.Start(context.Background()); !errors.Is(err, transport.ErrEndpointInUse) {
		t.Fatalf("Start error = %v, want ErrEndpointInUse", err)
	}
	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("runtime close calls = %d, want 1", runtime.closeCalls.Load())
	}
	if err := daemon.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after startup failure: %v", err)
	}
	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("runtime close calls after Stop = %d, want 1", runtime.closeCalls.Load())
	}
}
