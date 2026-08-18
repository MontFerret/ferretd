package exec

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/workspace"
)

func BenchmarkManagerCreateSessionUnchanged(b *testing.B) {
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN @value"), 0o600); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()
	workspaces := workspace.New()
	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		b.Fatalf("workspace Open: %v", err)
	}
	manager := New(workspaces)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		session, err := manager.CreateSession(ctx, opened.ID(), "query.fql")
		if err != nil {
			b.Fatalf("CreateSession: %v", err)
		}
		if err := manager.CloseSession(ctx, session.ID); err != nil {
			b.Fatalf("CloseSession: %v", err)
		}
	}

	b.StopTimer()
	_ = manager.Close(ctx)
	_ = workspaces.Clear(ctx)
}

func BenchmarkManagerExecutionSameSession(b *testing.B) {
	manager, session, workspaces := benchmarkExecutionManager(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		created, err := manager.CreateExecution(ctx, session.ID, map[string]any{"value": 1}, ExecutionOptions{})
		if err != nil {
			b.Fatalf("CreateExecution: %v", err)
		}
		subscription, err := manager.WatchExecution(ctx, created.ID)
		if err != nil {
			b.Fatalf("WatchExecution: %v", err)
		}
		if _, err := manager.RunExecution(ctx, created.ID); err != nil {
			b.Fatalf("RunExecution: %v", err)
		}
		for event := range subscription.Events {
			_ = event
		}
		subscription.Cancel()
		if err := manager.CloseExecution(ctx, created.ID); err != nil {
			b.Fatalf("CloseExecution: %v", err)
		}
	}

	b.StopTimer()
	_ = manager.Close(ctx)
	_ = workspaces.Clear(ctx)
}

func BenchmarkManagerExecutionSameSessionConcurrent(b *testing.B) {
	manager, session, workspaces := benchmarkExecutionManager(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			created, err := manager.CreateExecution(
				ctx,
				session.ID,
				map[string]any{"value": 1},
				ExecutionOptions{},
			)
			if err != nil {
				b.Errorf("CreateExecution: %v", err)

				return
			}
			subscription, err := manager.WatchExecution(ctx, created.ID)
			if err != nil {
				b.Errorf("WatchExecution: %v", err)

				return
			}
			if _, err := manager.RunExecution(ctx, created.ID); err != nil {
				b.Errorf("RunExecution: %v", err)

				return
			}
			for event := range subscription.Events {
				_ = event
			}
			subscription.Cancel()
			if err := manager.CloseExecution(ctx, created.ID); err != nil {
				b.Errorf("CloseExecution: %v", err)

				return
			}
		}
	})

	b.StopTimer()
	_ = manager.Close(ctx)
	_ = workspaces.Clear(ctx)
}

func benchmarkExecutionManager(b *testing.B) (*Manager, SessionSnapshot, *workspace.Manager) {
	b.Helper()

	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN @value"), 0o600); err != nil {
		b.Fatalf("WriteFile: %v", err)
	}
	ctx := context.Background()
	workspaces := workspace.New()
	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		b.Fatalf("workspace Open: %v", err)
	}
	manager := New(workspaces)
	session, err := manager.CreateSession(ctx, opened.ID(), "query.fql")
	if err != nil {
		b.Fatalf("CreateSession: %v", err)
	}

	return manager, session, workspaces
}
