package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/internal/transport"
)

func TestNew(t *testing.T) {
	d, err := New(Options{Endpoint: testEndpoint(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil daemon")
	}
	if d.workspaces == nil || d.executions == nil || d.grpc == nil {
		t.Fatal("New did not construct all service boundaries")
	}
}

func TestStartReturnsOnCancellationAndStopCleansUp(t *testing.T) {
	endpoint := testEndpoint(t)
	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Start(ctx)
	}()

	waitForEndpoint(t, endpoint)
	if _, err := d.workspaces.Open(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("Open workspace: %v", err)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after context cancellation")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	if err := d.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	workspaces, err := d.workspaces.List(context.Background())
	if err != nil || len(workspaces) != 0 {
		t.Fatalf("workspaces after Stop = %#v, %v; want empty", workspaces, err)
	}
	assertEndpointUnavailable(t, endpoint)
}

func TestStopIsIdempotentBeforeStart(t *testing.T) {
	d, err := New(Options{Endpoint: testEndpoint(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for range 2 {
		if err := d.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}
}

func TestStopRunningDaemonUnblocksStart(t *testing.T) {
	endpoint := testEndpoint(t)
	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- d.Start(context.Background())
	}()
	waitForEndpoint(t, endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-startDone; err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestStartupFailureLeavesStopSafe(t *testing.T) {
	endpoint := testEndpoint(t)
	listener, err := transport.Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = d.Start(context.Background())
	if !errors.Is(err, transport.ErrEndpointInUse) {
		t.Fatalf("Start error = %v, want ErrEndpointInUse", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after failed Start: %v", err)
	}
}

func TestUnexpectedServeFailureCanBeCleanedUp(t *testing.T) {
	endpoint := testEndpoint(t)
	d, err := New(Options{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- d.Start(context.Background())
	}()
	waitForEndpoint(t, endpoint)

	d.mu.Lock()
	listener := d.listener
	d.mu.Unlock()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("Start returned nil after unexpected listener failure")
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after listener failure")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	assertEndpointUnavailable(t, endpoint)
}

func waitForEndpoint(t *testing.T, endpoint transport.Endpoint) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	for ctx.Err() == nil {
		connection, err := transport.Dial(ctx, endpoint)
		if err == nil {
			_ = connection.Close()

			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("daemon endpoint did not become available")
}

func assertEndpointUnavailable(t *testing.T, endpoint transport.Endpoint) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	connection, err := transport.Dial(ctx, endpoint)
	if err == nil {
		_ = connection.Close()
		t.Fatal("daemon endpoint remains available")
	}
}
