package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/MontFerret/specs/pkg/api"
)

const standardLibraryID = "montferret/core"

func referenceURL(base *url.URL, index *api.Index, version string) (*url.URL, error) {
	for _, entry := range index.Versions {
		if entry.Version != version {
			continue
		}

		href, err := url.Parse(entry.Href)
		if err != nil {
			return nil, fmt.Errorf("parse Standard Library API Reference location for version %q: %w", version, err)
		}

		return base.ResolveReference(href), nil
	}

	return nil, fmt.Errorf("standard library API Reference index does not contain Ferret version %q", version)
}

func validateReference(data []byte, version string) (*api.Reference, error) {
	reference, err := api.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse Standard Library API Reference: %w", err)
	}

	if reference.SchemaVersion != api.SchemaVersion {
		return nil, fmt.Errorf("standard library API Reference schema version = %d, want %d", reference.SchemaVersion, api.SchemaVersion)
	}

	if reference.ID != standardLibraryID {
		return nil, fmt.Errorf("standard library API Reference ID = %q, want %q", reference.ID, standardLibraryID)
	}

	if reference.Version != version {
		return nil, fmt.Errorf("standard library API Reference version = %q, want %q", reference.Version, version)
	}

	return reference, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)

	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
	}()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(mode); err != nil {
		return err
	}

	if _, err := temporary.Write(data); err != nil {
		return err
	}

	if err := temporary.Sync(); err != nil {
		return err
	}

	if err := temporary.Close(); err != nil {
		return err
	}

	return os.Rename(temporaryPath, path)
}
