package source

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseURIPath(t *testing.T) {
	value := "file:///tmp/Ferret%20Lab/%23query-%C3%A9.fql"
	uri, err := ParseURI(value)
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if uri.String() != value {
		t.Fatalf("URI.String = %q, want %q", uri.String(), value)
	}

	got, err := uri.Path()
	if err != nil {
		t.Fatalf("URI.Path: %v", err)
	}

	want := filepath.FromSlash("/tmp/Ferret Lab/#query-é.fql")
	if got != want {
		t.Fatalf("URI.Path = %q, want %q", got, want)
	}

	uri, err = ParseURI("file://localhost/tmp/query.fql")
	if err != nil {
		t.Fatalf("ParseURI localhost: %v", err)
	}

	got, err = uri.Path()
	if err != nil {
		t.Fatalf("URI.Path localhost: %v", err)
	}
	if got != filepath.FromSlash("/tmp/query.fql") {
		t.Fatalf("URI.Path localhost = %q", got)
	}
}

func TestParseURIRejectsUnsupportedURI(t *testing.T) {
	for _, uri := range []string{
		"",
		"https://example.com/query.fql",
		"file://server/query.fql",
		"file:///tmp/query.fql?x=1",
		"file:///tmp/query.fql#fragment",
		"file:",
		"file:///tmp/%zz.fql",
	} {
		t.Run(uri, func(t *testing.T) {
			if _, err := ParseURI(uri); err == nil {
				t.Fatalf("ParseURI(%q) returned nil error", uri)
			}
		})
	}
}

func TestURIFromPathRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Ferret Lab", "#query-é.fql")

	uri, err := URIFromPath(path)
	if err != nil {
		t.Fatalf("URIFromPath: %v", err)
	}
	if !strings.HasPrefix(uri.String(), "file://") {
		t.Fatalf("URIFromPath = %q, want file URI", uri)
	}
	if strings.Contains(uri.String(), " ") || strings.Contains(uri.String(), "#") {
		t.Fatalf("URIFromPath = %q, want escaped URI", uri)
	}

	got, err := uri.Path()
	if err != nil {
		t.Fatalf("URI.Path: %v", err)
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(got, path) {
			t.Fatalf("round trip path = %q, want %q", got, path)
		}
	} else if got != path {
		t.Fatalf("round trip path = %q, want %q", got, path)
	}
}

func TestURIFromPathRejectsEmptyPath(t *testing.T) {
	if _, err := URIFromPath(""); err == nil {
		t.Fatal("URIFromPath returned nil error")
	}
}
