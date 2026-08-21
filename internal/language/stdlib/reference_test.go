package stdlib

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestReferenceIsEmbeddedAndIndependentOfWorkingDirectory(t *testing.T) {
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	reference := Reference()
	if reference.ID != "montferret/core" || reference.SchemaVersion != 1 || len(reference.Namespaces) == 0 {
		t.Fatalf("embedded reference = %+v", reference)
	}

	reference.Namespaces[0].Functions[0].Name = "mutated"
	if Reference().Namespaces[0].Functions[0].Name == "mutated" {
		t.Fatal("Reference exposed mutable embedded metadata")
	}
}

func TestReferenceVersionMatchesFerretModule(t *testing.T) {
	command := exec.Command("go", "list", "-mod=readonly", "-m", "-f", "{{.Version}}", "github.com/MontFerret/ferret/v2")
	data, err := command.Output()
	if err != nil {
		t.Fatalf("resolve Ferret module version: %v", err)
	}

	dependencyVersion := strings.TrimPrefix(strings.TrimSpace(string(data)), "v")
	if referenceVersion := Reference().Version; referenceVersion != dependencyVersion {
		t.Fatalf("embedded version = %q, dependency version = %q", referenceVersion, dependencyVersion)
	}
}

func TestMustParsePanicsForMalformedReference(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("mustParse did not panic")
		}
	}()

	mustParse([]byte(`{"schemaVersion":1}`))
}
