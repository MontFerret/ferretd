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

	localsource "github.com/MontFerret/ferretd/internal/source"
)

type workspaceContent struct {
	files     []File
	documents map[string]Document
	order     []string
}

func loadWorkspace(ctx context.Context, rootPath string) (workspaceContent, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return workspaceContent{}, fmt.Errorf("open workspace root: %w", err)
	}

	content, loadErr := loadWorkspaceFS(ctx, rootPath, root.FS())
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

func loadWorkspaceFS(ctx context.Context, rootPath string, fileSystem fs.FS) (workspaceContent, error) {
	files, err := discoverFiles(ctx, rootPath, fileSystem)
	if err != nil {
		return workspaceContent{}, err
	}

	content := workspaceContent{
		files:     files,
		documents: make(map[string]Document, len(files)),
		order:     make([]string, 0, len(files)),
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

func discoverFiles(ctx context.Context, rootPath string, fileSystem fs.FS) ([]File, error) {
	var result []File

	err := fs.WalkDir(fileSystem, ".", func(relativePath string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if walkErr != nil {
			return walkErr
		}

		if relativePath == "." {
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		if entry.IsDir() {
			if isExcludedDirectory(path.Base(relativePath)) {
				return fs.SkipDir
			}

			return nil
		}

		if path.Ext(relativePath) != ".fql" {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		canonical := path.Clean(relativePath)
		absolute := filepath.Join(rootPath, filepath.FromSlash(canonical))
		uri, err := localsource.PathToURI(absolute)
		if err != nil {
			return fmt.Errorf("resolve source URI for %q: %w", canonical, err)
		}

		result = append(result, File{
			RelativePath: canonical,
			Path:         absolute,
			URI:          localsource.URI(uri),
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover source files: %w", err)
	}

	sort.Slice(result, func(left, right int) bool {
		return result[left].RelativePath < result[right].RelativePath
	})

	return result, nil
}

func isExcludedDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor":
		return true
	default:
		return false
	}
}
