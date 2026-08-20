package stdlib

import (
	"encoding/json"
	"testing"

	"github.com/MontFerret/specs/pkg/api"
)

func TestDefaultParsesEmbeddedReferenceAndLooksUpFunctionsCaseInsensitively(t *testing.T) {
	reference := Default()
	if reference.Version() == "" {
		t.Fatal("embedded reference has empty version")
	}

	average, ok := reference.Lookup("AvErAgE")
	if !ok || average.Name != "average" || average.Namespace != "" || len(average.Signatures) == 0 {
		t.Fatalf("average metadata = %+v, found %t", average, ok)
	}

	read, ok := reference.Lookup("IO::FS::READ")
	if !ok || read.Name != "io::fs::read" || read.Namespace != "io::fs" {
		t.Fatalf("io::fs::read metadata = %+v, found %t", read, ok)
	}
}

func TestLookupReturnsDefensiveMetadataCopy(t *testing.T) {
	reference := Default()
	first, ok := reference.Lookup("average")
	if !ok || len(first.Signatures) == 0 || len(first.Signatures[0].Parameters) == 0 || first.Signatures[0].Return == nil {
		t.Fatalf("average metadata = %+v, found %t", first, ok)
	}

	first.Signatures[0].Parameters[0].Name = "changed"
	first.Signatures[0].Return.Type = "Changed"

	second, ok := reference.Lookup("average")
	if !ok || second.Signatures[0].Parameters[0].Name == "changed" || second.Signatures[0].Return.Type == "Changed" {
		t.Fatalf("lookup exposed mutable metadata: %+v", second)
	}

	functions := reference.Functions()
	for index := range functions {
		if functions[index].Name == "average" {
			functions[index].Signatures[0].Parameters[0].Name = "changed again"
			functions[index].Signatures[0].Return.Type = "ChangedAgain"

			break
		}
	}

	third, ok := reference.Lookup("average")
	if !ok || third.Signatures[0].Parameters[0].Name == "changed again" || third.Signatures[0].Return.Type == "ChangedAgain" {
		t.Fatalf("function list exposed mutable metadata: %+v", third)
	}
}

func TestParseRejectsNonCoreReference(t *testing.T) {
	data, err := json.Marshal(&api.Reference{
		SchemaVersion: api.SchemaVersion,
		ID:            "other/module",
		Version:       "1.0.0",
		Namespaces:    []api.Namespace{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Parse(data); err == nil {
		t.Fatal("non-Core reference parsed successfully")
	}
}
