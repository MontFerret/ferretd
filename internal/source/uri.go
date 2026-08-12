// Package source contains protocol-neutral source locations.
package source

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// URI identifies a source document.
type URI string

// URIToPath converts a local file URI into an operating-system path.
func URIToPath(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse URI: %w", err)
	}

	if !strings.EqualFold(parsed.Scheme, "file") {
		return "", fmt.Errorf("unsupported URI scheme %q", parsed.Scheme)
	}

	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("unsupported file URI host %q", parsed.Host)
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("file URI must not contain a query or fragment")
	}

	path := parsed.Path
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	if path == "" {
		return "", errors.New("file URI path is empty")
	}

	return filepath.FromSlash(path), nil
}

// PathToURI converts a local path into an escaped absolute file URI.
func PathToURI(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}

	slashPath := filepath.ToSlash(absolute)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}

	return (&url.URL{Scheme: "file", Path: slashPath}).String(), nil
}
