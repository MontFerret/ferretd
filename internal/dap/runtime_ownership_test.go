package dap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/MontFerret/ferretd/internal/workspace"
)

func TestServerClosesChildrenBeforeOwnedRuntimeExactlyOnce(t *testing.T) {
	runtime := newRuntimeOwnershipSpy()

	server, err := newServer(strings.NewReader(""), io.Discard, Options{}.normalized(), runtime)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 1"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opened, err := server.workspaces.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := server.executions.CreateSession(context.Background(), opened.ID(), "query.fql"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	server.workspaces.RegisterCloseHook(func(context.Context, workspace.ID) error {
		runtime.record("workspace")

		return nil
	})

	const callers = 8
	results := make(chan error, callers)
	var callersReady sync.WaitGroup
	callersReady.Add(callers)
	start := make(chan struct{})
	for range callers {
		go func() {
			callersReady.Done()
			<-start
			results <- server.cleanup()
		}()
	}

	callersReady.Wait()
	close(start)
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}

	if got, want := runtime.recordedEvents(), []string{"plan", "workspace", "runtime"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup events = %v, want %v", got, want)
	}

	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("runtime close calls = %d, want 1", runtime.closeCalls.Load())
	}
}

func TestServerRunCancellationClosesOwnedRuntime(t *testing.T) {
	runtime := newRuntimeOwnershipSpy()

	server, err := newServer(strings.NewReader(""), io.Discard, Options{}.normalized(), runtime)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}

	if runtime.closeCalls.Load() != 1 {
		t.Fatalf("runtime close calls = %d, want 1", runtime.closeCalls.Load())
	}
}
