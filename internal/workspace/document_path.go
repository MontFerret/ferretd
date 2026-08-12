package workspace

import (
	"path"
	"path/filepath"
)

func documentKey(relativePath string) (string, bool) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", false
	}

	key := path.Clean(filepath.ToSlash(relativePath))
	if key == "." || key == ".." || key[0] == '/' || (len(key) > 3 && key[:3] == "../") {
		return "", false
	}

	return key, true
}
