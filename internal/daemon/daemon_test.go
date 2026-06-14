package daemon

import (
	"context"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d == nil {
		t.Fatal("New returned a nil daemon")
	}
	if d.workspaces == nil || d.language == nil || d.execution == nil || d.debug == nil || d.lsp == nil {
		t.Fatal("New did not construct all service boundaries")
	}
}

func TestStartReturnsOnCancellation(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Start(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for range 2 {
		if err := d.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}
}
