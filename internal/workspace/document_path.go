package workspace

import (
	"path"
	"path/filepath"
)

func normalizeDocumentPath(relativePath string) (string, bool) {
	key, ok := normalizeWorkspacePath(relativePath)
	if !ok || key == "." {
		return "", false
	}

	return key, true
}

func normalizeWorkspacePath(relativePath string) (string, bool) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", false
	}

	key := path.Clean(filepath.ToSlash(relativePath))
	if key == ".." || key[0] == '/' || (len(key) > 3 && key[:3] == "../") {
		return "", false
	}

	return key, true
}

func workspacePathInSubtree(relativePath string, subtree string) bool {
	if subtree == "." {
		return true
	}

	return relativePath == subtree ||
		(len(relativePath) > len(subtree) && relativePath[:len(subtree)] == subtree &&
			relativePath[len(subtree)] == '/')
}
