package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestOpenIsIdempotentAndDoesNotResolveSymlinks(t *testing.T) {
	manager := New()
	root := t.TempDir()

	first, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	second, err := manager.Open(context.Background(), root+string(filepath.Separator))
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}

	if first != second {
		t.Fatalf("workspaces differ: %#v != %#v", first, second)
	}
	if _, err := uuid.Parse(string(first.ID)); err != nil {
		t.Fatalf("workspace ID is not a UUID: %v", err)
	}

	link := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	linked, err := manager.Open(context.Background(), link)
	if err != nil {
		t.Fatalf("Open symlink: %v", err)
	}
	if linked.ID == first.ID {
		t.Fatal("symlink root unexpectedly resolved to target identity")
	}
}

func TestOpenValidatesRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.fql")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tests := []struct {
		name string
		root string
	}{
		{name: "empty"},
		{name: "relative", root: "relative"},
		{name: "missing", root: filepath.Join(t.TempDir(), "missing")},
		{name: "file", root: file},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New().Open(context.Background(), tt.root)
			if !errors.Is(err, ErrInvalidRoot) {
				t.Fatalf("Open error = %v, want ErrInvalidRoot", err)
			}
		})
	}
}

func TestWorkspaceLifecycleAndOrdering(t *testing.T) {
	manager := New()
	parent := t.TempDir()
	roots := []string{
		filepath.Join(parent, "zeta"),
		filepath.Join(parent, "alpha"),
	}
	for _, root := range roots {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("mkdir %q: %v", root, err)
		}
	}

	zeta, err := manager.Open(context.Background(), roots[0])
	if err != nil {
		t.Fatalf("Open zeta: %v", err)
	}
	alpha, err := manager.Open(context.Background(), roots[1])
	if err != nil {
		t.Fatalf("Open alpha: %v", err)
	}

	items, err := manager.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 || items[0] != alpha || items[1] != zeta {
		t.Fatalf("List = %#v, want alpha then zeta", items)
	}

	got, err := manager.Get(context.Background(), zeta.ID)
	if err != nil || got != zeta {
		t.Fatalf("Get = %#v, %v; want %#v", got, err, zeta)
	}

	if err := manager.Close(context.Background(), zeta.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := manager.Close(context.Background(), zeta.ID); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
	if _, err := manager.Get(context.Background(), zeta.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get closed error = %v, want ErrNotFound", err)
	}

	manager.Clear()
	items, err = manager.List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("List after Clear = %#v, %v; want empty", items, err)
	}
}

func TestConcurrentOpenConverges(t *testing.T) {
	manager := New()
	root := t.TempDir()
	results := make(chan Workspace, 32)
	errors := make(chan error, 32)

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()

			result, err := manager.Open(context.Background(), root)
			results <- result
			errors <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
	}

	var want ID
	for result := range results {
		if want == "" {
			want = result.ID
		}
		if result.ID != want {
			t.Fatalf("workspace ID = %q, want %q", result.ID, want)
		}
	}
}

func TestOperationsRespectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	manager := New()
	if _, err := manager.Open(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want context.Canceled", err)
	}
	if _, err := manager.Get(ctx, "unknown"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get error = %v, want context.Canceled", err)
	}
	if _, err := manager.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List error = %v, want context.Canceled", err)
	}
	if err := manager.Close(ctx, "unknown"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close error = %v, want context.Canceled", err)
	}
}
