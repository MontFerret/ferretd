package debug

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func BenchmarkManagerDebugSessionSameSession(b *testing.B) {
	manager, executions, session, workspaces := benchmarkDebugManager(b)
	ctx := context.Background()

	warm, err := manager.CreateSession(ctx, session.ID, map[string]any{"value": 1}, exec.RuntimeOptions{})
	if err != nil {
		b.Fatalf("warm CreateSession: %v", err)
	}

	if err := manager.CloseSession(ctx, warm.ID); err != nil {
		b.Fatalf("warm CloseSession: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		created, err := manager.CreateSession(ctx, session.ID, map[string]any{"value": 1}, exec.RuntimeOptions{})
		if err != nil {
			b.Fatalf("CreateSession: %v", err)
		}

		subscription, err := manager.WatchSession(ctx, created.ID)
		if err != nil {
			b.Fatalf("WatchSession: %v", err)
		}

		if _, err := manager.StartSession(ctx, created.ID); err != nil {
			b.Fatalf("StartSession: %v", err)
		}

		for event := range subscription.Events {
			if event.Snapshot.State == StateStopped {
				if _, err := manager.ContinueSession(ctx, created.ID); err != nil {
					b.Fatalf("ContinueSession: %v", err)
				}
			}
		}

		subscription.Cancel()

		if err := manager.CloseSession(ctx, created.ID); err != nil {
			b.Fatalf("CloseSession: %v", err)
		}
	}

	b.StopTimer()
	_ = manager.Close(ctx)
	_ = executions.Close(ctx)
	_ = workspaces.Clear(ctx)
}

func benchmarkDebugManager(
	b *testing.B,
) (*Manager, *exec.Manager, exec.SessionSnapshot, *workspace.Manager) {
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

	runtime := newRuntimeSpy()
	executions := mustNewExecutionManager(b, workspaces, runtime)
	manager := mustNewManager(b, executions)

	session, err := executions.CreateSession(ctx, opened.ID(), "query.fql")
	if err != nil {
		b.Fatalf("CreateSession: %v", err)
	}

	return manager, executions, session, workspaces
}
