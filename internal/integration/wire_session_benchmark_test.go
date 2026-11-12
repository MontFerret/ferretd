package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MontFerret/ferretd/internal/exec"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/workspace"
)

func BenchmarkWireAcquireRelease(b *testing.B) {
	wire := newWireFixture(b, 41)
	owner := newWireOwner(b, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		binding, err := owner.Acquire(ctx, "application")
		if err != nil {
			b.Fatal(err)
		}

		if err := binding.Release(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWireCreateSession(b *testing.B) {
	wire := newWireFixture(b, 41)
	owner := newWireOwner(b, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true})
	root := b.TempDir()
	// Source setup is excluded from measurement, matching the native benchmark.
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN TASK3_APPLICATION_VALUE()"), 0600); err != nil {
		b.Fatal(err)
	}

	workspaces := workspace.New()
	ctx := context.Background()

	opened, err := workspaces.Open(ctx, root)
	if err != nil {
		b.Fatal(err)
	}

	manager, err := exec.New(workspaces, owner)
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = manager.Close(ctx); _ = workspaces.Clear(ctx) })
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		session, err := manager.CreateSession(ctx, opened.ID(), "query.fql", "application")
		if err != nil {
			b.Fatal(err)
		}

		if err := manager.CloseSession(ctx, session.ID); err != nil {
			b.Fatal(err)
		}
	}
}
