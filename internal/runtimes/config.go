// Package runtimes owns runtime configuration, acquisition, and root cleanup.
package runtimes

import (
	"fmt"
	"strings"
	"time"
)

type (
	// Config describes additional targets and an optional default. Its zero value
	// selects the implicit local runtime. Catalog construction copies its values.
	Config struct {
		DefaultRuntime *string
		Runtimes       []DefinitionConfig
	}

	// DefinitionConfig configures one additional Wire target. A nil timeout uses 5s.
	DefinitionConfig struct {
		Name           string
		Label          string
		Provider       Provider
		Endpoint       string
		Insecure       bool
		ConnectTimeout *string
	}
)

func (c DefinitionConfig) normalized() (Definition, error) {
	if c.Name == "" || strings.TrimSpace(c.Name) != c.Name {
		return Definition{}, fmt.Errorf("%w: runtime name must be nonempty without surrounding whitespace", ErrInvalidConfig)
	}

	if c.Name == LocalName {
		return Definition{}, fmt.Errorf("%w: runtime name %q is reserved", ErrInvalidConfig, c.Name)
	}

	if c.Provider != ProviderWire {
		return Definition{}, fmt.Errorf("%w: runtime %q has unsupported provider %q", ErrInvalidConfig, c.Name, c.Provider)
	}

	target, err := wireTarget(c.Endpoint)
	if err != nil {
		return Definition{}, fmt.Errorf("%w: runtime %q endpoint: %w", ErrInvalidConfig, c.Name, err)
	}

	timeout := 5 * time.Second

	if c.ConnectTimeout != nil {
		parsed, err := time.ParseDuration(*c.ConnectTimeout)
		if err != nil || parsed <= 0 {
			return Definition{}, fmt.Errorf("%w: runtime %q connect timeout must be a positive duration", ErrInvalidConfig, c.Name)
		}

		timeout = parsed
	}

	label := c.Label
	if label == "" {
		label = c.Name
	}

	return Definition{
		Name:           c.Name,
		Label:          label,
		Provider:       c.Provider,
		Endpoint:       c.Endpoint,
		Insecure:       c.Insecure,
		ConnectTimeout: timeout,
		target:         target,
	}, nil
}
