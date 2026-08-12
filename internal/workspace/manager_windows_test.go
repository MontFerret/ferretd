//go:build windows

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenUsesCasePreservingLexicalIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "CaseSensitiveIdentity")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	alternate := filepath.Join(filepath.Dir(root), strings.ToLower(filepath.Base(root)))
	if _, err := os.Stat(alternate); err != nil {
		t.Skipf("filesystem does not resolve case-variant path: %v", err)
	}

	manager := New()
	first, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open original root: %v", err)
	}

	second, err := manager.Open(context.Background(), alternate)
	if err != nil {
		t.Fatalf("Open case-variant root: %v", err)
	}

	if first.ID == second.ID {
		t.Fatalf("case-variant roots share workspace ID %q", first.ID)
	}

	if first.Root != filepath.Clean(root) || second.Root != filepath.Clean(alternate) {
		t.Fatalf("workspace roots = %q and %q, want lexical inputs", first.Root, second.Root)
	}
}
