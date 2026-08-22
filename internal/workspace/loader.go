package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	localsource "github.com/MontFerret/ferretd/internal/source"
)

type (
	workspaceContent struct {
		files       []File
		documents   map[string]Document
		order       []string
		directories []string
	}

	directoryObserver func(string) error
)

func loadWorkspace(
	ctx context.Context,
	rootPath string,
	observeDirectory directoryObserver,
) (workspaceContent, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return workspaceContent{}, fmt.Errorf("open workspace root: %w", err)
	}

	content, loadErr := loadWorkspaceFS(ctx, rootPath, root.FS(), observeDirectory)
	closeErr := root.Close()

	if loadErr != nil {
		if closeErr != nil {
			return workspaceContent{}, errors.Join(loadErr, fmt.Errorf("close workspace root: %w", closeErr))
		}

		return workspaceContent{}, loadErr
	}

	if closeErr != nil {
		return workspaceContent{}, fmt.Errorf("close workspace root: %w", closeErr)
	}

	return content, nil
}

func loadWorkspaceFS(
	ctx context.Context,
	rootPath string,
	fileSystem fs.FS,
	observers ...directoryObserver,
) (workspaceContent, error) {
	var observeDirectory directoryObserver
	if len(observers) != 0 {
		observeDirectory = observers[0]
	}

	return loadWorkspaceTreeFS(ctx, rootPath, fileSystem, ".", observeDirectory)
}

func loadWorkspaceSubtree(
	ctx context.Context,
	rootPath string,
	relativePath string,
	observeDirectory directoryObserver,
) (workspaceContent, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return workspaceContent{}, fmt.Errorf("open workspace root: %w", err)
	}

	key, ok := normalizeWorkspacePath(relativePath)
	if !ok {
		_ = root.Close()

		return workspaceContent{}, nil
	}

	if key != "." {
		eligible, err := validateWorkspaceDirectory(root, key)
		if err != nil {
			_ = root.Close()

			return workspaceContent{}, err
		}

		if !eligible {
			closeErr := root.Close()
			if closeErr != nil {
				return workspaceContent{}, fmt.Errorf("close workspace root: %w", closeErr)
			}

			return workspaceContent{documents: make(map[string]Document)}, nil
		}
	}

	content, loadErr := loadWorkspaceTreeFS(ctx, rootPath, root.FS(), key, observeDirectory)
	closeErr := root.Close()
	if loadErr != nil {
		if closeErr != nil {
			return workspaceContent{}, errors.Join(loadErr, fmt.Errorf("close workspace root: %w", closeErr))
		}

		return workspaceContent{}, loadErr
	}

	if closeErr != nil {
		return workspaceContent{}, fmt.Errorf("close workspace root: %w", closeErr)
	}

	return content, nil
}

func loadWorkspaceTreeFS(
	ctx context.Context,
	rootPath string,
	fileSystem fs.FS,
	start string,
	observeDirectory directoryObserver,
) (workspaceContent, error) {
	files, directories, err := discoverFiles(ctx, rootPath, fileSystem, start, observeDirectory)
	if err != nil {
		return workspaceContent{}, err
	}

	content := workspaceContent{
		files:       files,
		documents:   make(map[string]Document, len(files)),
		order:       make([]string, 0, len(files)),
		directories: directories,
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return workspaceContent{}, err
		}

		bytes, err := fs.ReadFile(fileSystem, file.RelativePath)
		if err != nil {
			content.documents[file.RelativePath] = newUnreadableDocument(file, fmt.Errorf("read %q: %w", file.RelativePath, err))
			content.order = append(content.order, file.RelativePath)

			continue
		}

		document := newDocument(file, string(bytes))
		if err := ctx.Err(); err != nil {
			return workspaceContent{}, err
		}

		content.documents[file.RelativePath] = document
		content.order = append(content.order, file.RelativePath)
	}

	return content, nil
}

func validateWorkspaceDirectory(root *os.Root, relativePath string) (bool, error) {
	current := "."
	components := splitWorkspacePath(relativePath)
	for index, component := range components {
		if isExcludedDirectory(component) {
			return false, nil
		}

		current = path.Join(current, component)
		info, err := root.Lstat(current)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}

			return false, fmt.Errorf("inspect directory %q: %w", current, err)
		}

		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, nil
		}

		if index == len(components)-1 {
			continue
		}

		entries, err := fs.ReadDir(root.FS(), current)
		if err != nil {
			return false, fmt.Errorf("read directory %q: %w", current, err)
		}

		if containsGoModule(entries) {
			return false, nil
		}
	}

	return true, nil
}

func discoverFiles(
	ctx context.Context,
	rootPath string,
	fileSystem fs.FS,
	start string,
	observeDirectory directoryObserver,
) ([]File, []string, error) {
	var result []File
	var directories []string

	var walk func(string, bool) error
	walk = func(relativePath string, selectedRoot bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		entries, err := fs.ReadDir(fileSystem, relativePath)
		if err != nil {
			return err
		}

		if observeDirectory != nil {
			if err := observeDirectory(relativePath); err != nil {
				return err
			}
		}
		directories = append(directories, path.Clean(relativePath))

		if !selectedRoot && containsGoModule(entries) {
			return nil
		}

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}

			child := path.Join(relativePath, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}

			if entry.IsDir() {
				if isExcludedDirectory(entry.Name()) {
					continue
				}

				if err := walk(child, false); err != nil {
					return err
				}

				continue
			}

			if !isWorkspaceSource(entry.Name()) {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				return err
			}

			if !info.Mode().IsRegular() {
				continue
			}

			canonical := path.Clean(child)
			absolute := filepath.Join(rootPath, filepath.FromSlash(canonical))
			uri, err := localsource.URIFromPath(absolute)
			if err != nil {
				return fmt.Errorf("resolve source URI for %q: %w", canonical, err)
			}

			result = append(result, File{
				RelativePath: canonical,
				Path:         absolute,
				URI:          uri,
			})
		}

		return nil
	}

	canonicalStart := path.Clean(start)
	selectedRoot := canonicalStart == "."
	if !selectedRoot && isExcludedDirectory(path.Base(canonicalStart)) {
		return nil, nil, nil
	}

	err := walk(canonicalStart, selectedRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("discover source files: %w", err)
	}

	sort.Slice(result, func(left, right int) bool {
		return result[left].RelativePath < result[right].RelativePath
	})

	return result, directories, nil
}

func isExcludedDirectory(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}

	switch name {
	case "node_modules", "testdata", "vendor":
		return true
	default:
		return false
	}
}

func containsGoModule(entries []fs.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() == "go.mod" && !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			return true
		}
	}

	return false
}

func isWorkspaceSource(name string) bool {
	return path.Ext(name) == ".fql"
}
