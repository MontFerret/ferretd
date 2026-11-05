package runtimes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStrictJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{"empty", ""}, {"null", "null"}, {"array", "[]"}, {"string", `"config"`},
		{"malformed", "{"}, {"unknown field", `{"other":1}`},
		{"trailing object", `{} {}`}, {"trailing null", `{} null`}, {"trailing junk", `{} junk`},
		{"null default", `{"defaultRuntime":null}`}, {"number default", `{"defaultRuntime":1}`},
		{"null runtimes", `{"runtimes":null}`}, {"object runtimes", `{"runtimes":{}}`},
		{"null entry", `{"runtimes":[null]}`}, {"string entry", `{"runtimes":["wire"]}`},
		{"unknown entry field", `{"runtimes":[{"other":true}]}`},
		{"null name", `{"runtimes":[{"name":null}]}`},
		{"null label", `{"runtimes":[{"label":null}]}`},
		{"null provider", `{"runtimes":[{"provider":null}]}`},
		{"null endpoint", `{"runtimes":[{"endpoint":null}]}`},
		{"null timeout", `{"runtimes":[{"connectTimeout":null}]}`},
		{"numeric timeout", `{"runtimes":[{"connectTimeout":5}]}`},
		{"numeric endpoint", `{"runtimes":[{"endpoint":5}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(t.Context(), writeConfig(t, test.data))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("Load = %v", err)
			}
		})
	}
}

func TestLoadAndCatalogDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	path := writeConfig(t, `{"runtimes":[{"name":"application","provider":"wire","endpoint":"arbitrary outbound syntax"}]}`)

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	relative, err := filepath.Rel(workingDirectory, path)
	if err != nil {
		t.Fatal(err)
	}

	config, err := Load(t.Context(), relative)
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := NewCatalog(config)
	if err != nil {
		t.Fatal(err)
	}

	definition, err := catalog.Resolve("application")
	if err != nil || definition.Label != "application" || definition.ConnectTimeout != 5*time.Second || catalog.DefaultName() != LocalName {
		t.Fatalf("loaded definition = %+v, %v", definition, err)
	}

	for _, data := range []string{`{}`, `{"defaultRuntime":"local","runtimes":[]}`, `{"runtimes":[{"name":"app","label":"My app","provider":"wire","endpoint":"tcp://127.0.0.1:9000","connectTimeout":"2s"}]}`} {
		config, err := Load(t.Context(), writeConfig(t, data))
		if err != nil {
			t.Fatal(err)
		}

		if _, err := NewCatalog(config); err != nil {
			t.Fatal(err)
		}
	}

	for _, data := range []string{`{"defaultRuntime":""}`, `{"runtimes":[{"name":"app","provider":"wire","endpoint":"x","connectTimeout":""}]}`} {
		config, err := Load(t.Context(), writeConfig(t, data))
		if err != nil {
			t.Fatal(err)
		}

		if _, err := NewCatalog(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("explicit blank = %v", err)
		}
	}
}

func TestLoadFailuresAndCancellation(t *testing.T) {
	for _, path := range []string{"", " ", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := Load(t.Context(), path); err == nil {
			t.Fatalf("Load(%q) succeeded", path)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := Load(ctx, "missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Load = %v", err)
	}

	if _, err := Load(nil, "missing"); !errors.Is(err, ErrInvalidConfig) { //nolint:staticcheck // Exercise the non-nil context contract.
		t.Fatalf("nil context = %v", err)
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	path := writeConfig(t, `{}`)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := Load(t.Context(), path)
	if err == nil {
		t.Skip("platform or user can read files without permission bits")
	}

	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("unreadable file error = %v", err)
	}
}

func writeConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "runtimes.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
