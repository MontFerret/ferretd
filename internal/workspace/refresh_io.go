package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	localsource "github.com/MontFerret/ferretd/internal/source"
)

type discoveredDocument struct {
	document    Document
	directories []string
	found       bool
}

func discoverWorkspaceDocument(
	ctx context.Context,
	rootPath string,
	relativePath string,
) (discoveredDocument, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return discoveredDocument{}, fmt.Errorf("open workspace root: %w", err)
	}

	result, discoverErr := discoverWorkspaceDocumentRoot(ctx, rootPath, root, relativePath)
	closeErr := root.Close()

	if discoverErr != nil {
		if closeErr != nil {
			return discoveredDocument{}, errors.Join(discoverErr, fmt.Errorf("close workspace root: %w", closeErr))
		}

		return discoveredDocument{}, discoverErr
	}

	if closeErr != nil {
		return discoveredDocument{}, fmt.Errorf("close workspace root: %w", closeErr)
	}

	return result, nil
}

func discoverWorkspaceDocumentRoot(
	ctx context.Context,
	rootPath string,
	root *os.Root,
	relativePath string,
) (discoveredDocument, error) {
	if err := ctx.Err(); err != nil {
		return discoveredDocument{}, err
	}

	key, ok := normalizeDocumentPath(relativePath)
	if !ok || !isWorkspaceSource(path.Base(key)) {
		return discoveredDocument{}, nil
	}

	directories, eligible, err := validateDocumentAncestors(root, key)
	if err != nil {
		return discoveredDocument{}, err
	}

	if !eligible {
		return discoveredDocument{directories: directories}, nil
	}

	pathInfo, err := root.Lstat(key)
	if err != nil {
		if workspacePathMissing(root.FS(), key, err) {
			return discoveredDocument{directories: directories}, nil
		}

		return discoveredDocument{}, fmt.Errorf("inspect %q: %w", key, err)
	}

	if !validSourceInfo(pathInfo) {
		return discoveredDocument{directories: directories}, nil
	}

	absolute := filepath.Join(rootPath, filepath.FromSlash(key))

	uri, err := localsource.URIFromPath(absolute)
	if err != nil {
		return discoveredDocument{}, fmt.Errorf("resolve source URI for %q: %w", key, err)
	}

	file := File{RelativePath: key, Path: absolute, URI: uri}

	document, found, err := readDiscoveredDocument(ctx, root, file, pathInfo)
	if err != nil {
		return discoveredDocument{}, err
	}

	return discoveredDocument{
		document:    document,
		directories: directories,
		found:       found,
	}, nil
}

func validateDocumentAncestors(root *os.Root, relativePath string) ([]string, bool, error) {
	directories := []string{"."}
	current := "."

	for _, component := range splitWorkspacePath(path.Dir(relativePath)) {
		if isExcludedDirectory(component) {
			return directories, false, nil
		}

		current = path.Join(current, component)

		info, err := root.Lstat(current)
		if err != nil {
			if workspacePathMissing(root.FS(), current, err) {
				return directories, false, nil
			}

			return nil, false, fmt.Errorf("inspect directory %q: %w", current, err)
		}

		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return directories, false, nil
		}

		entries, err := fs.ReadDir(root.FS(), current)
		if err != nil {
			if workspacePathMissing(root.FS(), current, err) {
				return directories, false, nil
			}

			return nil, false, fmt.Errorf("read directory %q: %w", current, err)
		}

		directories = append(directories, current)

		if containsGoModule(entries) {
			return directories, false, nil
		}
	}

	return directories, true, nil
}

func splitWorkspacePath(value string) []string {
	if value == "." || value == "" {
		return nil
	}

	return strings.Split(path.Clean(value), "/")
}

func readDiscoveredDocument(
	ctx context.Context,
	root *os.Root,
	file File,
	pathInfo os.FileInfo,
) (Document, bool, error) {
	handle, err := root.Open(file.RelativePath)
	if err != nil {
		if workspacePathMissing(root.FS(), file.RelativePath, err) {
			return Document{}, false, nil
		}

		return newUnreadableDocument(file, fmt.Errorf("open %q: %w", file.RelativePath, err)), true, nil
	}

	defer func() { _ = handle.Close() }()

	openedInfo, err := handle.Stat()
	if err != nil {
		return newUnreadableDocument(file, fmt.Errorf("inspect open %q: %w", file.RelativePath, err)), true, nil
	}

	currentInfo, err := root.Lstat(file.RelativePath)
	if err != nil {
		if workspacePathMissing(root.FS(), file.RelativePath, err) {
			return Document{}, false, nil
		}

		return newUnreadableDocument(file, fmt.Errorf("inspect %q: %w", file.RelativePath, err)), true, nil
	}

	if !validSourceInfo(currentInfo) || !openedInfo.Mode().IsRegular() ||
		!os.SameFile(openedInfo, currentInfo) || !os.SameFile(pathInfo, currentInfo) {
		return Document{}, false, nil
	}

	bytes, err := io.ReadAll(handle)
	if err != nil {
		return newUnreadableDocument(file, fmt.Errorf("read %q: %w", file.RelativePath, err)), true, nil
	}

	if err := ctx.Err(); err != nil {
		return Document{}, false, err
	}

	return newDocument(file, string(bytes)), true, nil
}

func validSourceInfo(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}
