package exec

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUniversalRuntimeImportBoundary(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}

			packagePath := filepath.ToSlash(filepath.Dir(path))
			if (packagePath == "../exec" || packagePath == "../debug") &&
				strings.HasPrefix(name, "github.com/MontFerret/ferret/v2") {
				t.Errorf("%s imports native Ferret runtime package %q", path, name)
			}
			if name == "github.com/MontFerret/ferret/v2" && packagePath != "../ferretapi" {
				t.Errorf("%s imports native Ferret runtime outside internal/ferretapi", path)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal packages: %v", err)
	}
}
