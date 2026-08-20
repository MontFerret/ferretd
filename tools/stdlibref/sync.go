package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MontFerret/specs/pkg/api"
)

const (
	ferretModulePath = "github.com/MontFerret/ferret/v2"
	referenceID      = "montferret/core"
	indexURL         = "https://ferretlang.org/ferret/index.json"
	maxDocumentSize  = 4 << 20
)

type (
	options struct {
		root     string
		output   string
		check    bool
		client   *http.Client
		indexURL string
	}

	moduleInfo struct {
		Path    string      `json:"Path"`
		Version string      `json:"Version"`
		Replace *moduleInfo `json:"Replace"`
	}
)

func run(ctx context.Context, opts options) error {
	version, err := resolveFerretVersion(ctx, opts.root)
	if err != nil {
		return err
	}

	output := opts.output
	if !filepath.IsAbs(output) {
		output = filepath.Join(opts.root, output)
	}

	if opts.check {
		return checkReference(output, version)
	}

	data, err := downloadReference(ctx, opts.client, opts.indexURL, version)
	if err != nil {
		return err
	}

	return writeReference(output, data)
}

func resolveFerretVersion(ctx context.Context, root string) (string, error) {
	command := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-m", "-json", ferretModulePath)
	command.Dir = root

	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("resolve Ferret module with go list: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}

		return "", fmt.Errorf("resolve Ferret module with go list: %w", err)
	}

	return parseFerretModule(output)
}

func parseFerretModule(data []byte) (string, error) {
	var module moduleInfo
	if err := json.Unmarshal(data, &module); err != nil {
		return "", fmt.Errorf("decode Ferret module information: %w", err)
	}

	if module.Path != ferretModulePath {
		return "", fmt.Errorf("resolved module path is %q, want %q", module.Path, ferretModulePath)
	}

	if module.Replace != nil {
		return "", fmt.Errorf("module replacements cannot be matched to a published standard library API reference")
	}

	if !strings.HasPrefix(module.Version, "v") || len(module.Version) == 1 {
		return "", fmt.Errorf("resolved Ferret module version %q is not a versioned dependency", module.Version)
	}

	return strings.TrimPrefix(module.Version, "v"), nil
}

func checkReference(path, version string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read embedded Standard Library API Reference: %w; run make generate", err)
	}

	if _, err := validateReference(data, version); err != nil {
		return fmt.Errorf("embedded Standard Library API Reference is stale or invalid: %w; run make generate", err)
	}

	return nil
}

func downloadReference(ctx context.Context, client *http.Client, indexLocation, version string) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is nil")
	}

	indexData, err := download(ctx, client, indexLocation)
	if err != nil {
		return nil, fmt.Errorf("download Ferret API Reference index: %w", err)
	}

	index, err := api.ParseIndex(indexData)
	if err != nil {
		return nil, fmt.Errorf("parse Ferret API Reference index: %w", err)
	}

	var href string
	for _, entry := range index.Versions {
		if entry.Version == version {
			href = entry.Href

			break
		}
	}

	if href == "" {
		return nil, fmt.Errorf("standard library API reference version %q is not published", version)
	}

	artifactURL, err := resolveArtifactURL(indexLocation, href)
	if err != nil {
		return nil, err
	}

	data, err := download(ctx, client, artifactURL)
	if err != nil {
		return nil, fmt.Errorf("download Ferret Standard Library API Reference %q: %w", version, err)
	}

	if _, err := validateReference(data, version); err != nil {
		return nil, err
	}

	return data, nil
}

func resolveArtifactURL(indexLocation, href string) (string, error) {
	base, err := url.Parse(indexLocation)
	if err != nil {
		return "", fmt.Errorf("parse canonical Ferret API Reference index URL: %w", err)
	}

	reference, err := url.Parse(href)
	if err != nil {
		return "", fmt.Errorf("parse Ferret API Reference href %q: %w", href, err)
	}

	resolved := base.ResolveReference(reference)
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host || resolved.Scheme == "" || resolved.Host == "" ||
		resolved.User != nil || resolved.RawQuery != "" || resolved.Fragment != "" || !strings.HasPrefix(resolved.EscapedPath(), "/ferret/") {
		return "", fmt.Errorf("API reference href %q resolves outside the canonical artifact origin", href)
	}

	return resolved.String(), nil
}

func download(ctx context.Context, client *http.Client, location string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request to %s returned %s", location, response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxDocumentSize+1))
	if err != nil {
		return nil, fmt.Errorf("read GET %s response: %w", location, err)
	}

	if len(data) > maxDocumentSize {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", location, maxDocumentSize)
	}

	return data, nil
}

func validateReference(data []byte, version string) (*api.Reference, error) {
	reference, err := api.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse standard library API reference: %w", err)
	}

	if reference.ID != referenceID {
		return nil, fmt.Errorf("standard library API reference id is %q, want %q", reference.ID, referenceID)
	}

	if reference.Version != version {
		return nil, fmt.Errorf("standard library API reference version is %q, want %q", reference.Version, version)
	}

	return reference, nil
}

func writeReference(path string, data []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return nil
	}

	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing Standard Library API Reference: %w", err)
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create Standard Library API Reference directory: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".api-*.json")
	if err != nil {
		return fmt.Errorf("create temporary Standard Library API Reference: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("set temporary Standard Library API Reference permissions: %w", err)
	}

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("write temporary Standard Library API Reference: %w", err)
	}

	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("sync temporary Standard Library API Reference: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Standard Library API Reference: %w", err)
	}

	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Standard Library API Reference: %w", err)
	}
	removeTemporary = false

	return nil
}
