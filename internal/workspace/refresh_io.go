package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
)

type documentRead struct {
	content string
	err     error
}

func readDocument(ctx context.Context, rootPath string, file File) (documentRead, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return documentRead{err: fmt.Errorf("open workspace root: %w", err)}, nil
	}
	defer func() { _ = root.Close() }()

	pathInfo, err := root.Lstat(file.RelativePath)
	if err != nil {
		return documentRead{err: fmt.Errorf("inspect %q: %w", file.RelativePath, err)}, nil
	}

	if err := validateRefreshFile(file.RelativePath, pathInfo); err != nil {
		return documentRead{err: err}, nil
	}

	handle, err := root.Open(file.RelativePath)
	if err != nil {
		return documentRead{err: fmt.Errorf("open %q: %w", file.RelativePath, err)}, nil
	}
	defer func() { _ = handle.Close() }()

	openedInfo, err := handle.Stat()
	if err != nil {
		return documentRead{err: fmt.Errorf("inspect open %q: %w", file.RelativePath, err)}, nil
	}

	pathInfo, err = root.Lstat(file.RelativePath)
	if err != nil {
		return documentRead{err: fmt.Errorf("inspect %q: %w", file.RelativePath, err)}, nil
	}

	if err := validateRefreshFile(file.RelativePath, pathInfo); err != nil {
		return documentRead{err: err}, nil
	}

	if !openedInfo.Mode().IsRegular() {
		return documentRead{err: fmt.Errorf("inspect %q: source is not a regular file", file.RelativePath)}, nil
	}

	if !os.SameFile(openedInfo, pathInfo) {
		return documentRead{err: fmt.Errorf("inspect %q: source changed while opening", file.RelativePath)}, nil
	}

	bytes, err := io.ReadAll(handle)
	if err != nil {
		return documentRead{err: fmt.Errorf("read %q: %w", file.RelativePath, err)}, nil
	}

	if err := ctx.Err(); err != nil {
		return documentRead{}, err
	}

	return documentRead{content: string(bytes)}, nil
}

func validateRefreshFile(relativePath string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("inspect %q: symbolic links are not supported", relativePath)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("inspect %q: source is not a regular file", relativePath)
	}

	return nil
}
