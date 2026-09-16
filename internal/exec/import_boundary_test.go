package exec

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestUniversalRuntimeImportBoundary(t *testing.T) {
	const native = "github.com/MontFerret/ferret"
	// These APIs provide editor analysis and retained syntax, outside the
	// Universal execution/debugger contract.
	tooling := map[string][]string{
		"internal/language":   {"compiler", "diagnostics", "formatter", "runtime", "source", "stdlib"},
		"internal/lsp":        {"compiler"},
		"internal/workspace":  {"diagnostics", "parser", "parser/diagnostics", "parser/fql", "source"},
		"internal/diagnostic": {"diagnostics", "source"},
	}

	root := filepath.Join("..", "..")

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "bin" {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		relative = filepath.ToSlash(relative)
		directory := filepath.ToSlash(filepath.Dir(relative))
		testFile := strings.HasSuffix(relative, "_test.go")

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}

			if name != native && !strings.HasPrefix(name, native+"/") {
				continue
			}

			if testFile && directory == "internal/integration" {
				continue
			}

			if (directory == "internal/daemon" || directory == "internal/dap") &&
				(testFile || filepath.Base(relative) == "composition.go") && name == native+"/v2/uapi" {
				continue
			}

			if suffix, ok := strings.CutPrefix(name, native+"/v2/pkg/"); ok &&
				slices.Contains(tooling[directory], suffix) {
				continue
			}

			t.Errorf("%s imports native Ferret outside composition, integration tests, or documented tooling: %q", relative, name)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
}
