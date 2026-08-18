package dap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLaunchPathsDefaultsRootAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	gotRoot, gotProgram, relative, err := resolveLaunchPaths(launchArguments{Program: program})
	if err != nil {
		t.Fatalf("resolveLaunchPaths: %v", err)
	}
	if gotRoot != root || gotProgram != program || relative != "query.fql" {
		t.Fatalf("resolved = %q, %q, %q", gotRoot, gotProgram, relative)
	}

	other := t.TempDir()
	if _, _, _, err := resolveLaunchPaths(launchArguments{Program: program, CWD: other}); err == nil {
		t.Fatal("program outside cwd was accepted")
	}
}

func TestResolveLaunchPathsSupportsProgramRelativeToCWD(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDAPProgram(t, nested, "RETURN 1")

	gotRoot, gotProgram, relative, err := resolveLaunchPaths(launchArguments{
		Program: filepath.Join("nested", "query.fql"),
		CWD:     root,
	})
	if err != nil {
		t.Fatalf("resolveLaunchPaths: %v", err)
	}
	if gotRoot != root || gotProgram != filepath.Join(nested, "query.fql") || relative != "nested/query.fql" {
		t.Fatalf("resolved = %q, %q, %q", gotRoot, gotProgram, relative)
	}
}

func TestSourcePathRejectsRemoteURIs(t *testing.T) {
	server := &Server{client: clientOptions{pathFormat: "uri"}}
	if _, err := server.sourcePath("file://remote/tmp/query.fql"); err == nil {
		t.Fatal("remote file URI was accepted")
	}
	if _, err := server.sourcePath("https://example.com/query.fql"); err == nil {
		t.Fatal("remote URI was accepted")
	}
}
