package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/MontFerret/specs/pkg/api"
)

const (
	indexLocation     = "https://ferretlang.org/ferret/index.json"
	maxIndexBytes     = 1 << 20
	maxReferenceBytes = 16 << 20
)

type command struct {
	output         string
	index          *url.URL
	client         *http.Client
	resolveVersion func(context.Context) (string, error)
}

func newCommand(output string, timeout time.Duration) (*command, error) {
	index, err := url.Parse(indexLocation)
	if err != nil {
		return nil, fmt.Errorf("parse Standard Library API Reference index URL: %w", err)
	}

	return &command{
		output: output,
		index:  index,
		client: &http.Client{Timeout: timeout},
		resolveVersion: func(ctx context.Context) (string, error) {
			return resolveFerretVersion(ctx, runGoModuleCommand)
		},
	}, nil
}

func (c *command) run(ctx context.Context, mode string) error {
	switch mode {
	case "sync":
		return c.sync(ctx)
	case "check":
		return c.check(ctx)
	default:
		return fmt.Errorf("unsupported mode %q: expected sync or check", mode)
	}
}

func (c *command) sync(ctx context.Context) error {
	version, err := c.resolveVersion(ctx)
	if err != nil {
		return err
	}

	indexData, err := c.fetch(ctx, c.index, maxIndexBytes)
	if err != nil {
		return fmt.Errorf("download Standard Library API Reference index: %w", err)
	}

	index, err := api.ParseIndex(indexData)
	if err != nil {
		return fmt.Errorf("parse Standard Library API Reference index: %w", err)
	}

	referenceURL, err := referenceURL(c.index, index, version)
	if err != nil {
		return err
	}

	referenceData, err := c.fetch(ctx, referenceURL, maxReferenceBytes)
	if err != nil {
		return fmt.Errorf("download Standard Library API Reference %q: %w", version, err)
	}

	if _, err := validateReference(referenceData, version); err != nil {
		return err
	}

	if err := writeAtomic(c.output, referenceData, 0o644); err != nil {
		return fmt.Errorf("write embedded Standard Library API Reference: %w", err)
	}

	return nil
}

func (c *command) check(ctx context.Context) error {
	version, err := c.resolveVersion(ctx)
	if err != nil {
		return err
	}

	referenceData, err := os.ReadFile(c.output)
	if err != nil {
		return fmt.Errorf("read embedded Standard Library API Reference: %w", err)
	}

	_, err = validateReference(referenceData, version)

	return err
}

func (c *command) fetch(ctx context.Context, location *url.URL, maximum int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("response exceeds %d bytes", maximum)
	}

	return data, nil
}
