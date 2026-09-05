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
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		testFile := strings.HasSuffix(path, "_test.go")

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
			if packagePath == "../debug" &&
				strings.HasPrefix(name, "github.com/MontFerret/ferret/v2") {
				t.Errorf("%s imports native Ferret runtime package %q", path, name)
			}
			if testFile {
				continue
			}
			if packagePath == "../exec" && strings.HasPrefix(name, "github.com/MontFerret/ferret/v2") {
				t.Errorf("%s imports native Ferret runtime package %q", path, name)
			}
			if packagePath == "../dap" && strings.HasPrefix(name, "github.com/MontFerret/ferret/v2/") {
				t.Errorf("%s imports native Ferret implementation package %q", path, name)
			}
			if packagePath == "../grpc" && strings.HasPrefix(name, "github.com/MontFerret/ferret/v2") {
				t.Errorf("%s imports native Ferret runtime package %q", path, name)
			}
			if name == "github.com/MontFerret/ferret/v2" && packagePath != "../ferretapi" &&
				packagePath != "../daemon" && packagePath != "../dap" {
				t.Errorf("%s imports native Ferret runtime outside composition or internal/ferretapi", path)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal packages: %v", err)
	}
}
