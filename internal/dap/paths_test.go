package dap

import (
	"os"
	"path/filepath"
	"strings"
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

func TestResolveLaunchPathsRejectsUnavailableProgram(t *testing.T) {
	root := t.TempDir()

	_, err := (launchArguments{Program: "missing.fql", CWD: root}).resolvePaths()
	if err == nil || !strings.Contains(err.Error(), "canonicalize program source") {
		t.Fatalf("resolvePaths error = %v, want canonicalization failure", err)
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

func TestSourceIdentityRecognizesEquivalentPaths(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}

	want, err := newSourceIdentity(program, root)
	if err != nil {
		t.Fatalf("newSourceIdentity: %v", err)
	}

	equivalent := []struct {
		name string
		path string
	}{
		{name: "absolute", path: program},
		{name: "relative", path: filepath.Base(program)},
		{
			name: "current_directory",
			path: root + string(filepath.Separator) + "." + string(filepath.Separator) + filepath.Base(program),
		},
		{
			name: "parent_directory",
			path: nested + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(program),
		},
	}
	for _, test := range equivalent {
		t.Run(test.name, func(t *testing.T) {
			got, identityErr := newSourceIdentity(test.path, root)
			if identityErr != nil {
				t.Fatalf("newSourceIdentity(%q): %v", test.path, identityErr)
			}

			if !got.same(want) {
				t.Fatalf("identity %q (%q) does not match %q (%q)", got.path, got.canonical, want.path, want.canonical)
			}
		})
	}
}

func TestSourceIdentityResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	programIdentity, err := newSourceIdentity(program, root)
	if err != nil {
		t.Fatalf("newSourceIdentity: %v", err)
	}

	link := filepath.Join(root, "linked.fql")
	if err := os.Symlink(program, link); err != nil {
		t.Skipf("create source symlink: %v", err)
	}

	linkIdentity, err := newSourceIdentity(link, root)
	if err != nil {
		t.Fatalf("newSourceIdentity symlink: %v", err)
	}

	if !linkIdentity.same(programIdentity) {
		t.Fatalf("symlink identity %+v does not match program identity %+v", linkIdentity, programIdentity)
	}
}

func TestSourceIdentityUsesFilesystemIdentity(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	hardLink := filepath.Join(root, "hard-link.fql")
	if err := os.Link(program, hardLink); err != nil {
		t.Skipf("create source hard link: %v", err)
	}

	programIdentity, err := newSourceIdentity(program, root)
	if err != nil {
		t.Fatalf("newSourceIdentity program: %v", err)
	}

	hardLinkIdentity, err := newSourceIdentity(hardLink, root)
	if err != nil {
		t.Fatalf("newSourceIdentity hard link: %v", err)
	}

	if programIdentity.canonical == hardLinkIdentity.canonical {
		t.Fatal("hard-linked sources unexpectedly have the same canonical path")
	}

	if !programIdentity.same(hardLinkIdentity) {
		t.Fatal("hard-linked sources have different filesystem identities")
	}
}

func TestSourceIdentityRespectsDistinctFiles(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	other := filepath.Join(root, "other.fql")
	if err := os.WriteFile(other, []byte("RETURN 2"), 0o600); err != nil {
		t.Fatal(err)
	}

	programIdentity, err := newSourceIdentity(program, root)
	if err != nil {
		t.Fatalf("newSourceIdentity program: %v", err)
	}

	otherIdentity, err := newSourceIdentity(other, root)
	if err != nil {
		t.Fatalf("newSourceIdentity other: %v", err)
	}

	if programIdentity.same(otherIdentity) {
		t.Fatal("distinct source files have the same identity")
	}
}

func TestSourceIdentityReturnsResolvedPathWithUnavailableError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing.fql")

	identity, err := newSourceIdentity(filepath.Base(path), root)
	if err == nil {
		t.Fatal("missing source was accepted")
	}

	if identity.path != path || identity.canonical != "" || identity.info != nil {
		t.Fatalf("partial identity = %+v, want resolved path %q only", identity, path)
	}
}

func TestSourceIdentityUsesPlatformCaseSemantics(t *testing.T) {
	root := t.TempDir()

	program := filepath.Join(root, "MixedCase.fql")
	if err := os.WriteFile(program, []byte("RETURN 1"), 0o600); err != nil {
		t.Fatal(err)
	}

	variant := filepath.Join(root, strings.ToLower(filepath.Base(program)))
	if _, err := os.Stat(variant); err != nil {
		t.Skip("test filesystem is case-sensitive")
	}

	programIdentity, err := newSourceIdentity(program, root)
	if err != nil {
		t.Fatalf("newSourceIdentity program: %v", err)
	}

	variantIdentity, err := newSourceIdentity(variant, root)
	if err != nil {
		t.Fatalf("newSourceIdentity variant: %v", err)
	}

	if !programIdentity.same(variantIdentity) {
		t.Fatal("case variants on a case-insensitive filesystem have different identities")
	}
}
