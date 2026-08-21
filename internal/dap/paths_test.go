package dap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLaunchPathsDefaultsRootAndRejectsEscape(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	paths, err := (launchArguments{Program: program}).resolvePaths()
	if err != nil {
		t.Fatalf("resolveLaunchPaths: %v", err)
	}
	if paths.root != root || paths.program != program || paths.relativePath != "query.fql" {
		t.Fatalf("resolved = %+v", paths)
	}

	other := t.TempDir()
	if _, err := (launchArguments{Program: program, CWD: other}).resolvePaths(); err == nil {
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

	paths, err := (launchArguments{
		Program: filepath.Join("nested", "query.fql"),
		CWD:     root,
	}).resolvePaths()
	if err != nil {
		t.Fatalf("resolveLaunchPaths: %v", err)
	}
	if paths.root != root || paths.program != filepath.Join(nested, "query.fql") || paths.relativePath != "nested/query.fql" {
		t.Fatalf("resolved = %+v", paths)
	}
}

func TestSourcePathRejectsRemoteURIs(t *testing.T) {
	server := &Server{client: clientOptions{pathFormat: pathFormatURI}}
	if _, err := server.sourcePath("file://remote/tmp/query.fql"); err == nil {
		t.Fatal("remote file URI was accepted")
	}
	if _, err := server.sourcePath("https://example.com/query.fql"); err == nil {
		t.Fatal("remote URI was accepted")
	}
}
