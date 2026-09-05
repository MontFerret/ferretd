package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
)

func watcherDirectoryInfo(absolutePath string, selectedRoot bool) (os.FileInfo, error) {
	var info os.FileInfo
	var err error

	if selectedRoot {
		info, err = os.Stat(absolutePath)
	} else {
		info, err = os.Lstat(absolutePath)
	}

	if err != nil {
		return nil, err
	}

	if !info.IsDir() || (!selectedRoot && info.Mode()&os.ModeSymlink != 0) {
		return nil, fs.ErrInvalid
	}

	return info, nil
}

func watcherPathAtOrBelow(candidate string, prefix string) bool {
	if candidate == prefix {
		return true
	}

	return len(candidate) > len(prefix) && candidate[:len(prefix)] == prefix &&
		candidate[len(prefix)] == filepath.Separator
}
