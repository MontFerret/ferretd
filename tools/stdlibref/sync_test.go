package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MontFerret/specs/pkg/api"
)

func TestResolveFerretVersionMatchesCheckedInReference(t *testing.T) {
	root := repositoryRoot(t)
	version, err := resolveFerretVersion(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if err := checkReference(filepath.Join(root, defaultOutput), version); err != nil {
		t.Fatal(err)
	}
}

func TestParseFerretModule(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "selected version", data: `{"Path":"github.com/MontFerret/ferret/v2","Version":"v2.0.0-alpha.49"}`, want: "2.0.0-alpha.49"},
		{name: "replacement", data: `{"Path":"github.com/MontFerret/ferret/v2","Version":"v2.0.0","Replace":{"Path":"../ferret"}}`},
		{name: "missing version", data: `{"Path":"github.com/MontFerret/ferret/v2"}`},
		{name: "wrong module", data: `{"Path":"example.com/ferret","Version":"v2.0.0"}`},
		{name: "invalid JSON", data: `{`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseFerretModule([]byte(test.data))
			if test.want == "" {
				if err == nil {
					t.Fatalf("parseFerretModule() = %q, want error", got)
				}

				return
			}

			if err != nil || got != test.want {
				t.Fatalf("parseFerretModule() = %q, %v, want %q", got, err, test.want)
			}
		})
	}
}

func TestDownloadReferenceSelectsExactPublishedVersion(t *testing.T) {
	const version = "2.0.0-alpha.49"
	referenceData := referenceData(t, version)
	indexData := indexData(t, []api.IndexVersion{
		{Version: version, Href: "./versions/" + version + "/api.json"},
		{Version: "2.0.0-alpha.48", Href: "./versions/2.0.0-alpha.48/api.json"},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ferret/index.json", func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(indexData)
	})
	mux.HandleFunc("/ferret/versions/"+version+"/api.json", func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(referenceData)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	got, err := downloadReference(context.Background(), server.Client(), server.URL+"/ferret/index.json", version)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(referenceData) {
		t.Fatal("downloaded reference bytes differ")
	}
}

func TestDownloadReferenceRejectsMissingAndInvalidArtifacts(t *testing.T) {
	tests := []struct {
		name            string
		indexStatus     int
		artifactStatus  int
		published       bool
		artifactID      string
		artifactVersion string
		want            string
	}{
		{name: "index status", indexStatus: http.StatusBadGateway, artifactStatus: http.StatusOK, published: true, artifactVersion: "1.0.0", want: "502 Bad Gateway"},
		{name: "unpublished", indexStatus: http.StatusOK, artifactStatus: http.StatusOK, want: "is not published"},
		{name: "artifact status", indexStatus: http.StatusOK, artifactStatus: http.StatusNotFound, published: true, artifactVersion: "1.0.0", want: "404 Not Found"},
		{name: "id mismatch", indexStatus: http.StatusOK, artifactStatus: http.StatusOK, published: true, artifactID: "other/module", artifactVersion: "1.0.0", want: `id is "other/module", want "montferret/core"`},
		{name: "version mismatch", indexStatus: http.StatusOK, artifactStatus: http.StatusOK, published: true, artifactVersion: "1.0.1", want: `version is "1.0.1", want "1.0.0"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := []api.IndexVersion{{Version: "0.9.0", Href: "./versions/0.9.0/api.json"}}
			if test.published {
				entries = append([]api.IndexVersion{{Version: "1.0.0", Href: "./versions/1.0.0/api.json"}}, entries...)
			}
			publishedIndex := indexData(t, entries)
			artifactID := test.artifactID
			if artifactID == "" {
				artifactID = referenceID
			}
			artifact := referenceDataWithID(t, artifactID, test.artifactVersion)

			mux := http.NewServeMux()
			mux.HandleFunc("/ferret/index.json", func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.indexStatus)
				if test.indexStatus == http.StatusOK {
					_, _ = response.Write(publishedIndex)
				}
			})
			mux.HandleFunc("/ferret/versions/1.0.0/api.json", func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.artifactStatus)
				if test.artifactStatus == http.StatusOK {
					_, _ = response.Write(artifact)
				}
			})
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)

			_, err := downloadReference(context.Background(), server.Client(), server.URL+"/ferret/index.json", "1.0.0")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDownloadReferenceRejectsInvalidIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"schemaVersion":1,"unexpected":true}`))
	}))
	t.Cleanup(server.Close)

	_, err := downloadReference(context.Background(), server.Client(), server.URL+"/ferret/index.json", "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "parse Ferret API Reference index") {
		t.Fatalf("error = %v, want strict index parsing failure", err)
	}
}

func TestResolveArtifactURLConfinesPublishedHref(t *testing.T) {
	const base = "https://ferretlang.org/ferret/index.json"

	got, err := resolveArtifactURL(base, "./versions/1.0.0/api.json")
	if err != nil || got != "https://ferretlang.org/ferret/versions/1.0.0/api.json" {
		t.Fatalf("resolved URL = %q, %v", got, err)
	}

	for _, href := range []string{
		"https://example.com/api.json",
		"../api.json",
		"./versions/1.0.0/api.json?raw=true",
		"./versions/1.0.0/api.json#fragment",
	} {
		if got, err := resolveArtifactURL(base, href); err == nil {
			t.Errorf("resolveArtifactURL(%q) = %q, want error", href, got)
		}
	}
}

func TestRunDoesNotReplaceExistingArtifactAfterInvalidDownload(t *testing.T) {
	root := repositoryRoot(t)
	version, err := resolveFerretVersion(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	index := indexData(t, []api.IndexVersion{{Version: version, Href: "./versions/" + version + "/api.json"}})
	mux := http.NewServeMux()
	mux.HandleFunc("/ferret/index.json", func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(index)
	})
	mux.HandleFunc("/ferret/versions/"+version+"/api.json", func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"invalid":true}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	output := filepath.Join(t.TempDir(), "api.json")
	const original = "original artifact"
	if err := os.WriteFile(output, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	err = run(context.Background(), options{
		root:     root,
		output:   output,
		client:   server.Client(),
		indexURL: server.URL + "/ferret/index.json",
	})
	if err == nil {
		t.Fatal("invalid download returned nil error")
	}

	data, readErr := os.ReadFile(output)
	if readErr != nil || string(data) != original {
		t.Fatalf("existing artifact = %q, %v", data, readErr)
	}
}

func TestCheckReferenceReportsStaleVersionAndRegenerationCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.json")
	if err := os.WriteFile(path, referenceData(t, "1.0.0"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := checkReference(path, "1.0.1")
	if err == nil || !strings.Contains(err.Error(), `version is "1.0.0", want "1.0.1"`) ||
		!strings.Contains(err.Error(), "run make generate") {
		t.Fatalf("check error = %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	return root
}

func referenceData(t *testing.T, version string) []byte {
	t.Helper()

	return referenceDataWithID(t, referenceID, version)
}

func referenceDataWithID(t *testing.T, id, version string) []byte {
	t.Helper()

	reference := &api.Reference{
		SchemaVersion: api.SchemaVersion,
		ID:            id,
		Version:       version,
		Namespaces:    []api.Namespace{},
	}
	data, err := json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func indexData(t *testing.T, versions []api.IndexVersion) []byte {
	t.Helper()

	index := &api.Index{SchemaVersion: api.IndexSchemaVersion, Versions: versions}
	for _, version := range versions {
		if !strings.Contains(version.Version, "-") {
			index.Latest = version.Version

			break
		}
	}

	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}

	return data
}
