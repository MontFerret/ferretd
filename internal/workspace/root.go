package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

func canonicalRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: root must be absolute", ErrInvalidRoot)
	}

	canonical := filepath.Clean(root)
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: stat root: %v", ErrInvalidRoot, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("%w: root is not a directory", ErrInvalidRoot)
	}

	return canonical, nil
}
