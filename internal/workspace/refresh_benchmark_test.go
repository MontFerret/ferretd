package workspace

import (
	"context"
	"testing"
)

func BenchmarkWorkspaceRefresh(b *testing.B) {
	for _, tree := range []bool{false, true} {
		name := "document"

		if tree {
			name = "tree"
		}

		b.Run(name, func(b *testing.B) {
			root := benchmarkWorkspaceRoot(b, 100)
			manager := New()
			manager.startWatcher = func(*workspaceWatcher, *Workspace) {}

			opened, err := manager.Open(context.Background(), root)
			if err != nil {
				b.Fatal(err)
			}

			b.Cleanup(func() {
				if err := manager.Clear(context.Background()); err != nil {
					b.Error(err)
				}
			})

			if _, err := opened.RefreshDocument(context.Background(), "dir-00/query-000.fql"); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if tree {
					if err := opened.reconcileTree(context.Background(), "."); err != nil {
						b.Fatal(err)
					}
				} else if _, err := opened.RefreshDocument(context.Background(), "dir-00/query-000.fql"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
