package runtimes

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNativeVersionMatchesDependency(t *testing.T) {
	command := exec.CommandContext(t.Context(), "go", "list", "-mod=readonly", "-m", "-f", "{{.Version}}", "github.com/MontFerret/ferret/v2")

	data, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}

	if want := strings.TrimPrefix(strings.TrimSpace(string(data)), "v"); NativeVersion.String() != want {
		t.Fatalf("native version = %q, dependency = %q", NativeVersion, want)
	}
}
