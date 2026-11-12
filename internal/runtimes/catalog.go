package runtimes

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MontFerret/api"
)

type (
	// Provider identifies the runtime implementation described by a definition.
	Provider string

	// Definition is a normalized startup target, including private connection settings.
	// It describes configuration, not connectivity or supported runtime capabilities.
	Definition struct {
		Name           string
		Label          string
		Provider       Provider
		Endpoint       string
		Insecure       bool
		ConnectTimeout time.Duration
		target         string
	}

	// Catalog is an immutable startup snapshot. It owns no runtime or connection.
	Catalog struct {
		defaultName string
		definitions map[string]Definition
		ordered     []Definition
	}

	// Factory constructs an owned runtime after startup configuration validation.
	Factory func() (api.Runtime, error)
)

const (
	// LocalName is reserved for the implicit built-in target.
	LocalName = "local"
	// ProviderNative identifies the built-in Ferret runtime.
	ProviderNative Provider = "native"
	// ProviderWire identifies an externally configured Wire target.
	ProviderWire Provider = "wire"
)

// NewCatalog validates configuration without constructing or connecting runtimes.
// Wire defaults are valid; catalog construction never acquires a runtime.
func NewCatalog(config Config) (*Catalog, error) {
	c := &Catalog{
		defaultName: LocalName,
		definitions: map[string]Definition{
			LocalName: {Name: LocalName, Label: "Built-in Ferret", Provider: ProviderNative},
		},
	}

	if config.DefaultRuntime != nil {
		c.defaultName = *config.DefaultRuntime
		if strings.TrimSpace(c.defaultName) == "" {
			return nil, fmt.Errorf("%w: default runtime must not be blank", ErrInvalidConfig)
		}
	}

	for _, entry := range config.Runtimes {
		definition, err := entry.normalized()
		if err != nil {
			return nil, err
		}

		if _, exists := c.definitions[definition.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate runtime name %q", ErrInvalidConfig, definition.Name)
		}

		c.definitions[definition.Name] = definition
	}

	if _, err := c.Resolve(""); err != nil {
		return nil, fmt.Errorf("%w: default runtime: %w", ErrInvalidConfig, err)
	}

	c.ordered = make([]Definition, 0, len(c.definitions))
	for _, definition := range c.definitions {
		c.ordered = append(c.ordered, definition)
	}

	slices.SortFunc(c.ordered, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })

	return c, nil
}

// Resolve selects an exact name, or the configured default for an empty selection.
// Unknown selections never fall back to another target.
func (c *Catalog) Resolve(name string) (Definition, error) {
	if name == "" {
		name = c.defaultName
	}

	definition, ok := c.definitions[name]
	if !ok {
		return Definition{}, fmt.Errorf("%w: %q", ErrRuntimeNotFound, name)
	}

	return definition, nil
}

// DefaultName returns the single authoritative default target name.
func (c *Catalog) DefaultName() string {
	return c.defaultName
}

// List returns a fresh snapshot ordered by case-sensitive runtime name.
func (c *Catalog) List() []Definition {
	return slices.Clone(c.ordered)
}
