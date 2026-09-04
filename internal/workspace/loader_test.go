package workspace

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/parser/fql"

	"github.com/MontFerret/ferretd/internal/source"
)

func TestOpenDiscoversLoadsAndParsesDocuments(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "zeta.fql", "RETURN 2")
	writeWorkspaceFile(t, root, "nested/alpha.fql", "RETURN missing")
	writeWorkspaceFile(t, root, "nested/invalid.fql", "RETURN")
	writeWorkspaceFile(t, root, "notes.txt", "ignored")
	writeWorkspaceFile(t, root, "upper.FQL", "ignored")
	writeWorkspaceFile(t, root, ".git/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, ".hg/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, ".svn/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, ".hidden/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "_generated/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "node_modules/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "testdata/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "vendor/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "nested-module/go.mod", "module example.com/nested")
	writeWorkspaceFile(t, root, "nested-module/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "go.mod", "module example.com/root")
	if err := os.Mkdir(filepath.Join(root, "directory.fql"), 0o700); err != nil {
		t.Fatalf("Mkdir directory.fql: %v", err)
	}

	manager := New()
	workspace, err := manager.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if workspace.State() != StateReady {
		t.Fatalf("workspace state = %v, want StateReady", workspace.State())
	}

	files := workspace.Files()
	wantPaths := []string{"nested/alpha.fql", "nested/invalid.fql", "zeta.fql"}
	if got := relativePaths(files); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("relative paths = %#v, want %#v", got, wantPaths)
	}

	documents := workspace.Documents()
	if len(documents) != len(wantPaths) {
		t.Fatalf("documents = %d, want %d", len(documents), len(wantPaths))
	}

	for i, document := range documents {
		file := document.File()
		if file != files[i] {
			t.Fatalf("document file = %#v, want %#v", file, files[i])
		}
		if document.Revision() != 1 || !document.Loaded() || !document.HasSyntax() {
			t.Fatalf("document state = revision %d loaded %t syntax %t", document.Revision(), document.Loaded(), document.HasSyntax())
		}

		wantURI, err := source.URIFromPath(file.Path)
		if err != nil {
			t.Fatalf("URIFromPath: %v", err)
		}
		if file.URI != wantURI {
			t.Fatalf("file URI = %q, want %q", file.URI, wantURI)
		}
	}

	alpha, ok := workspace.Document("nested/../nested/alpha.fql")
	if !ok {
		t.Fatal("Document alpha not found")
	}
	if alpha.Content() != "RETURN missing" {
		t.Fatalf("alpha content = %q", alpha.Content())
	}
	sourceCopy := alpha.Source()
	if err := sourceCopy.UnmarshalJSON([]byte(`{"name":"mutated","text":"changed","lines":["changed"]}`)); err != nil {
		t.Fatalf("mutate source copy: %v", err)
	}
	if alpha.Content() != "RETURN missing" {
		t.Fatal("source copy mutation changed retained document state")
	}
	if diagnostics := alpha.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("syntax-valid compiler-invalid diagnostics = %#v, want none", diagnostics)
	}
	if _, ok := alpha.VisitSyntax(&fql.BaseFqlParserVisitor{}); !ok {
		t.Fatal("VisitSyntax did not visit retained parse state")
	}
	if _, ok := workspace.Document("../zeta.fql"); ok {
		t.Fatal("Document resolved a path outside the workspace")
	}

	invalid, ok := workspace.Document("nested/invalid.fql")
	if !ok {
		t.Fatal("Document invalid not found")
	}
	diagnostics := invalid.Diagnostics()
	if len(diagnostics) == 0 || diagnostics[0].Kind.String() != "SyntaxError" {
		t.Fatalf("invalid diagnostics = %#v, want syntax error", diagnostics)
	}
	if diagnostics[0].Source.Empty() || diagnostics[0].Source.Name() != invalid.File().Path {
		t.Fatalf("diagnostic source = %#v, want %q", diagnostics[0].Source, invalid.File().Path)
	}

	allDiagnostics := workspace.Diagnostics()
	if len(allDiagnostics) != len(diagnostics) {
		t.Fatalf("workspace diagnostics = %d, want %d", len(allDiagnostics), len(diagnostics))
	}

	diagnostics[0].Message = "mutated"
	if got := invalid.Diagnostics()[0].Message; got == "mutated" {
		t.Fatal("diagnostic mutation changed retained document state")
	}

	files[0].RelativePath = "mutated.fql"
	if got := workspace.Files()[0].RelativePath; got == "mutated.fql" {
		t.Fatal("file mutation changed retained workspace state")
	}

	if err := manager.Close(context.Background(), workspace.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if workspace.State() != StateClosed || len(workspace.Files()) != 0 || len(workspace.Documents()) != 0 || len(workspace.Diagnostics()) != 0 {
		t.Fatalf("closed workspace retained source state")
	}
}

func TestOpenKeepsSelectedRootValidRegardlessOfName(t *testing.T) {
	parent := t.TempDir()
	tests := []string{".hidden", "_generated", "testdata", "vendor"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(parent, name)
			writeWorkspaceFile(t, root, "query.fql", "RETURN 1")

			manager := newTestManager(t)
			opened, err := manager.Open(context.Background(), root)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			if _, ok := opened.Document("query.fql"); !ok {
				t.Fatal("selected root source was excluded")
			}
		})
	}
}

func TestOpenPreservesRootSymlinkAndSkipsNestedSymlinks(t *testing.T) {
	target := t.TempDir()
	writeWorkspaceFile(t, target, "inside.fql", "RETURN 1")

	linkParent := t.TempDir()
	rootLink := filepath.Join(linkParent, "linked-root")
	if err := os.Symlink(target, rootLink); err != nil {
		t.Skipf("create root symlink: %v", err)
	}

	outside := t.TempDir()
	writeWorkspaceFile(t, outside, "outside.fql", "RETURN 2")
	if err := os.Symlink(outside, filepath.Join(target, "linked-directory")); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.fql"), filepath.Join(target, "linked-file.fql")); err != nil {
		t.Skipf("create file symlink: %v", err)
	}

	manager := newTestManager(t)
	workspace, err := manager.Open(context.Background(), rootLink)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if workspace.Root() != filepath.Clean(rootLink) {
		t.Fatalf("root = %q, want lexical symlink %q", workspace.Root(), rootLink)
	}

	files := workspace.Files()
	if got, want := relativePaths(files), []string{"inside.fql"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
	if files[0].Path != filepath.Join(rootLink, "inside.fql") {
		t.Fatalf("file path = %q, want symlink-root identity", files[0].Path)
	}
}

func TestLoadWorkspaceRetainsUnreadableDocumentDiagnostic(t *testing.T) {
	readErr := errors.New("permission denied")
	fileSystem := readErrorFS{
		FS: fstest.MapFS{
			"valid.fql":  {Data: []byte("RETURN 1")},
			"broken.fql": {Data: []byte("RETURN 2")},
		},
		path: "broken.fql",
		err:  readErr,
	}

	content, err := loadWorkspaceFS(context.Background(), t.TempDir(), fileSystem)
	if err != nil {
		t.Fatalf("loadWorkspaceFS: %v", err)
	}
	if len(content.documents) != 2 {
		t.Fatalf("documents = %d, want 2", len(content.documents))
	}

	broken := content.documents["broken.fql"]
	if broken.Loaded() || broken.HasSyntax() {
		t.Fatalf("broken state = loaded %t syntax %t", broken.Loaded(), broken.HasSyntax())
	}
	diagnostics := broken.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Kind != ferretdiagnostics.UnexpectedError || !errors.Is(diagnostics[0], readErr) {
		t.Fatalf("broken diagnostics = %#v, want retained read error", diagnostics)
	}
	if valid := content.documents["valid.fql"]; !valid.Loaded() || !valid.HasSyntax() {
		t.Fatalf("valid document = loaded %t syntax %t", valid.Loaded(), valid.HasSyntax())
	}
}

func TestLoadWorkspaceRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := loadWorkspaceFS(ctx, t.TempDir(), fstest.MapFS{"query.fql": {Data: []byte("RETURN 1")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadWorkspaceFS error = %v, want context.Canceled", err)
	}
}

func TestWorkspacePathMissing(t *testing.T) {
	fileSystem := fstest.MapFS{
		"present/query.fql": {Data: []byte("RETURN 1")},
	}
	tests := []struct {
		name         string
		relativePath string
		err          error
		want         bool
	}{
		{name: "not exist", relativePath: "missing", err: fs.ErrNotExist, want: true},
		{name: "delete pending", relativePath: "missing", err: fs.ErrPermission, want: true},
		{name: "retained permission", relativePath: "present", err: fs.ErrPermission, want: false},
		{name: "mixed permission", relativePath: "missing", err: errors.Join(fs.ErrPermission, errors.New("inspect directory")), want: false},
		{name: "unrelated", relativePath: "missing", err: errors.New("inspect directory"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := workspacePathMissing(fileSystem, test.relativePath, test.err); got != test.want {
				t.Fatalf("workspacePathMissing = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLoadWorkspaceRootPrunesVanishedNestedDirectory(t *testing.T) {
	root := t.TempDir()
	fileSystem := fstest.MapFS{
		"kept.fql":         {Data: []byte("RETURN 1")},
		"nested/query.fql": {Data: []byte("RETURN 2")},
	}
	var observed []string

	content, err := loadWorkspaceFS(
		context.Background(),
		root,
		fileSystem,
		func(relativePath string) error {
			observed = append(observed, relativePath)
			if relativePath == "nested" {
				return os.NewSyscallError("GetFileAttributes", fs.ErrNotExist)
			}

			return nil
		},
	)
	if err != nil {
		t.Fatalf("loadWorkspaceFS: %v", err)
	}
	if got, want := observed, []string{".", "nested"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observed directories = %#v, want %#v", got, want)
	}
	if got, want := content.directories, []string{"."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("retained directories = %#v, want %#v", got, want)
	}
	if got, want := relativePaths(content.files), []string{"kept.fql"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("relative paths = %#v, want %#v", got, want)
	}
	if got, want := content.order, []string{"kept.fql"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("document order = %#v, want %#v", got, want)
	}
	if kept, found := content.documents["kept.fql"]; !found || kept.Content() != "RETURN 1" {
		t.Fatalf("kept document = %#v, %t", kept, found)
	}
	if _, found := content.documents["nested/query.fql"]; found {
		t.Fatal("vanished nested document was retained")
	}
}

func TestLoadWorkspaceSubtreeTreatsVanishedDirectoryAsEmpty(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "nested/query.fql", "RETURN 1")

	content, err := loadWorkspaceSubtree(
		context.Background(),
		root,
		"nested",
		func(string) error {
			return os.NewSyscallError("GetFileAttributes", fs.ErrNotExist)
		},
	)
	if err != nil {
		t.Fatalf("loadWorkspaceSubtree: %v", err)
	}
	if content.documents == nil {
		t.Fatal("vanished subtree documents map is nil")
	}
	if len(content.files) != 0 || len(content.documents) != 0 || len(content.order) != 0 || len(content.directories) != 0 {
		t.Fatalf("vanished subtree content = %+v, want initialized empty content", content)
	}
}

func TestLoadWorkspaceSubtreePreservesRootAndObservationErrors(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "nested/query.fql", "RETURN 1")

	t.Run("root disappearance", func(t *testing.T) {
		_, err := loadWorkspaceSubtree(
			context.Background(),
			root,
			".",
			func(string) error {
				return fs.ErrNotExist
			},
		)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadWorkspaceSubtree error = %v, want fs.ErrNotExist", err)
		}
	})

	t.Run("unrelated nested error", func(t *testing.T) {
		observeErr := errors.New("observe directory")
		_, err := loadWorkspaceSubtree(
			context.Background(),
			root,
			"nested",
			func(string) error {
				return observeErr
			},
		)
		if !errors.Is(err, observeErr) {
			t.Fatalf("loadWorkspaceSubtree error = %v, want %v", err, observeErr)
		}
	})

	t.Run("mixed nested error", func(t *testing.T) {
		observeErr := errors.New("remove stale watch")
		_, err := loadWorkspaceSubtree(
			context.Background(),
			root,
			"nested",
			func(string) error {
				return errors.Join(fs.ErrNotExist, observeErr)
			},
		)
		if !errors.Is(err, observeErr) {
			t.Fatalf("loadWorkspaceSubtree error = %v, want %v", err, observeErr)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		_, err := loadWorkspaceSubtree(
			ctx,
			root,
			"nested",
			func(string) error {
				cancel()

				return fs.ErrNotExist
			},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("loadWorkspaceSubtree error = %v, want context.Canceled", err)
		}
	})
}

func writeWorkspaceFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatalf("MkdirAll %q: %v", relativePath, err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %q: %v", relativePath, err)
	}
}

func relativePaths(files []File) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, file.RelativePath)
	}

	return result
}
