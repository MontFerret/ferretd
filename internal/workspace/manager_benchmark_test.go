package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkManagerOpen(b *testing.B) {
	tests := []struct {
		name  string
		files int
	}{
		{name: "empty"},
		{name: "nested_100", files: 100},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			root := benchmarkWorkspaceRoot(b, tt.files)
			ctx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				manager := New()

				opened, err := manager.Open(ctx, root)
				if err != nil {
					b.Fatalf("Open: %v", err)
				}

				if err := manager.Close(ctx, opened.ID()); err != nil {
					b.Fatalf("Close: %v", err)
				}
			}
		})
	}
}

func benchmarkWorkspaceRoot(b *testing.B, files int) string {
	b.Helper()

	root := b.TempDir()
	for i := range files {
		directory := filepath.Join(root, fmt.Sprintf("dir-%02d", i%10))
		if err := os.MkdirAll(directory, 0o700); err != nil {
			b.Fatalf("MkdirAll: %v", err)
		}

		path := filepath.Join(directory, fmt.Sprintf("query-%03d.fql", i))
		if err := os.WriteFile(path, []byte("RETURN 1"), 0o600); err != nil {
			b.Fatalf("WriteFile: %v", err)
		}
	}

	return root
}
