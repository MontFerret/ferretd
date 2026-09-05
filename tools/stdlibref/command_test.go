package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MontFerret/specs/pkg/api"
)

func TestSyncDownloadsExactReferenceAndCheckStaysOffline(t *testing.T) {
	version := "2.0.0-alpha.49"
	referenceData := testReferenceData(t, standardLibraryID, version)

	output := filepath.Join(t.TempDir(), "api.json")
	command := testCommand(t, "https://example.test/index.json", output, version)

	command.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/index.json":
			return testHTTPResponse(http.StatusOK, testIndexData(t, version)), nil
		case "/versions/" + version + "/api.json":
			return testHTTPResponse(http.StatusOK, referenceData), nil
		default:
			return testHTTPResponse(http.StatusNotFound, nil), nil
		}
	})
	if err := command.sync(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != string(referenceData) {
		t.Fatalf("written reference = %q, want %q", got, referenceData)
	}

	command.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network used during check")
	})
	if err := command.check(context.Background()); err != nil {
		t.Fatalf("offline check: %v", err)
	}
}

func TestSyncRejectsInvalidInputsWithoutReplacingOutput(t *testing.T) {
	version := "2.0.0-alpha.49"
	tests := []struct {
		name            string
		indexStatus     int
		referenceStatus int
		index           []byte
		referenceData   []byte
	}{
		{name: "index HTTP failure", indexStatus: http.StatusBadGateway},
		{name: "reference HTTP failure", index: testIndexData(t, version), referenceStatus: http.StatusBadGateway},
		{name: "malformed index", index: []byte(`{`)},
		{name: "missing version", index: testIndexData(t, "2.0.0-alpha.48")},
		{name: "malformed reference", index: testIndexData(t, version), referenceData: []byte(`{`)},
		{name: "wrong identity", index: testIndexData(t, version), referenceData: testReferenceData(t, "acme/core", version)},
		{name: "wrong version", index: testIndexData(t, version), referenceData: testReferenceData(t, standardLibraryID, "2.0.0-alpha.48")},
		{name: "wrong schema", index: testIndexData(t, version), referenceData: []byte(`{"schemaVersion":2,"id":"montferret/core","version":"2.0.0-alpha.49","namespaces":[]}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "api.json")
			command := testCommand(t, "https://example.test/index.json", output, version)
			command.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/index.json" {
					if test.indexStatus != 0 {
						return testHTTPResponse(test.indexStatus, nil), nil
					}

					return testHTTPResponse(http.StatusOK, test.index), nil
				}

				status := test.referenceStatus
				if status == 0 {
					status = http.StatusOK
				}

				return testHTTPResponse(status, test.referenceData), nil
			})

			if err := os.WriteFile(output, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := command.sync(context.Background()); err == nil {
				t.Fatal("sync returned nil error")
			}

			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}

			if string(got) != "original" {
				t.Fatalf("failed sync replaced output with %q", got)
			}
		})
	}
}

func TestCheckRejectsStaleReferenceWithoutNetwork(t *testing.T) {
	output := filepath.Join(t.TempDir(), "api.json")
	if err := os.WriteFile(output, testReferenceData(t, standardLibraryID, "2.0.0-alpha.48"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := testCommand(t, "https://invalid.example/index.json", output, "2.0.0-alpha.49")
	command.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network used during check")
	})

	err := command.check(context.Background())
	if err == nil || !strings.Contains(err.Error(), `version = "2.0.0-alpha.48", want "2.0.0-alpha.49"`) {
		t.Fatalf("check error = %v", err)
	}
}

func testCommand(t testing.TB, indexLocation, output, version string) *command {
	t.Helper()

	index, err := url.Parse(indexLocation)
	if err != nil {
		t.Fatal(err)
	}

	return &command{
		output: output,
		index:  index,
		client: http.DefaultClient,
		resolveVersion: func(context.Context) (string, error) {
			return version, nil
		},
	}
}

func testIndexData(t testing.TB, version string) []byte {
	t.Helper()

	data, err := json.Marshal(&api.Index{
		SchemaVersion: api.IndexSchemaVersion,
		Versions: []api.IndexVersion{{
			Version: version,
			Href:    "./versions/" + version + "/api.json",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func testReferenceData(t testing.TB, id, version string) []byte {
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

func testHTTPResponse(status int, data []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}
