package source

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestURIToPath(t *testing.T) {
	got, err := URIToPath("file:///tmp/Ferret%20Lab/%23query-%C3%A9.fql")
	if err != nil {
		t.Fatalf("URIToPath: %v", err)
	}

	want := filepath.FromSlash("/tmp/Ferret Lab/#query-é.fql")
	if got != want {
		t.Fatalf("URIToPath = %q, want %q", got, want)
	}

	got, err = URIToPath("file://localhost/tmp/query.fql")
	if err != nil {
		t.Fatalf("URIToPath localhost: %v", err)
	}
	if got != filepath.FromSlash("/tmp/query.fql") {
		t.Fatalf("URIToPath localhost = %q", got)
	}
}

func TestURIToPathRejectsUnsupportedURI(t *testing.T) {
	for _, uri := range []string{
		"https://example.com/query.fql",
		"file://server/query.fql",
		"file:///tmp/query.fql?x=1",
		"file:///tmp/query.fql#fragment",
	} {
		t.Run(uri, func(t *testing.T) {
			if _, err := URIToPath(uri); err == nil {
				t.Fatalf("URIToPath(%q) returned nil error", uri)
			}
		})
	}
}

func TestPathToURIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Ferret Lab", "#query-é.fql")

	uri, err := PathToURI(path)
	if err != nil {
		t.Fatalf("PathToURI: %v", err)
	}
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("PathToURI = %q, want file URI", uri)
	}
	if strings.Contains(uri, " ") || strings.Contains(uri, "#") {
		t.Fatalf("PathToURI = %q, want escaped URI", uri)
	}

	got, err := URIToPath(uri)
	if err != nil {
		t.Fatalf("URIToPath: %v", err)
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(got, path) {
			t.Fatalf("round trip path = %q, want %q", got, path)
		}
	} else if got != path {
		t.Fatalf("round trip path = %q, want %q", got, path)
	}
}
