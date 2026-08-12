package workspace

import (
	"context"
	"errors"
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
	writeWorkspaceFile(t, root, "node_modules/ignored.fql", "RETURN 1")
	writeWorkspaceFile(t, root, "vendor/ignored.fql", "RETURN 1")
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

		wantURI, err := source.PathToURI(file.Path)
		if err != nil {
			t.Fatalf("PathToURI: %v", err)
		}
		if file.URI != source.URI(wantURI) {
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
	if diagnostics[0].Source == nil || diagnostics[0].Source.Name() != invalid.File().Path {
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

	workspace, err := New().Open(context.Background(), rootLink)
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
