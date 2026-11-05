package runtimes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Load reads one explicit JSON configuration file relative to the process working
// directory. It preserves omission for catalog defaults; it does not build a catalog.
func Load(ctx context.Context, path string) (Config, error) {
	if ctx == nil {
		return Config{}, fmt.Errorf("%w: nil context", ErrInvalidConfig)
	}

	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("%w: runtime configuration path must not be blank", ErrInvalidConfig)
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return Config{}, fmt.Errorf("resolve runtime configuration path: %w", err)
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		return Config{}, fmt.Errorf("read runtime configuration %q: %w", absolute, err)
	}

	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	var file fileConfig
	if err := decodeConfigObject(data, &file); err != nil {
		return Config{}, fmt.Errorf("%w: decode %q: %w", ErrInvalidConfig, absolute, err)
	}

	config, err := file.config()
	if err != nil {
		return Config{}, fmt.Errorf("%w: decode %q: %w", ErrInvalidConfig, absolute, err)
	}

	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	return config, nil
}

func decodeConfigObject(data []byte, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("expected a JSON object")
	}

	return decodeConfigJSON(trimmed, destination)
}

func decodeConfigJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON data")
	}

	return nil
}
