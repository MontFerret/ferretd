package runtimes

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCatalogLocalDefault(t *testing.T) {
	catalog, err := NewCatalog(Config{})
	if err != nil {
		t.Fatal(err)
	}

	want := Definition{Name: "local", Label: "Built-in Ferret", Provider: ProviderNative}

	definition, err := catalog.Resolve("")
	if err != nil || definition != want || catalog.DefaultName() != "local" {
		t.Fatalf("default = %+v, %v; name = %q", definition, err, catalog.DefaultName())
	}

	if got := catalog.List(); !reflect.DeepEqual(got, []Definition{want}) {
		t.Fatalf("List = %+v", got)
	}
}

func TestCatalogResolutionAndIsolation(t *testing.T) {
	defaultName, timeout := "remote", "2s"
	config := Config{
		DefaultRuntime: &defaultName,
		Runtimes: []DefinitionConfig{
			{Name: "remote", Provider: ProviderWire, Endpoint: "not parsed here", ConnectTimeout: &timeout},
			{Name: "Remote", Label: "Other", Provider: ProviderWire, Endpoint: "tcp://unavailable.invalid:9000"},
		},
	}

	catalog, err := NewCatalog(config)
	if err != nil {
		t.Fatal(err)
	}

	defaultName, timeout = "local", "9s"
	config.Runtimes[0].Name = "changed"

	listed := catalog.List()
	if got := []string{listed[0].Name, listed[1].Name, listed[2].Name}; !reflect.DeepEqual(got, []string{"Remote", "local", "remote"}) {
		t.Fatalf("order = %v", got)
	}

	listed[2].Endpoint = "changed"

	definition, err := catalog.Resolve("")
	if err != nil || definition.Name != "remote" || definition.Label != "remote" || definition.ConnectTimeout != 2*time.Second || definition.Endpoint != "not parsed here" {
		t.Fatalf("Resolve = %+v, %v", definition, err)
	}

	definition.Name = "changed"

	other, err := catalog.Resolve("Remote")
	if err != nil || other.ConnectTimeout != 5*time.Second || other.Label != "Other" {
		t.Fatalf("explicit resolution = %+v, %v", other, err)
	}

	for _, name := range []string{"REMOTE", "missing", " remote"} {
		got, err := catalog.Resolve(name)
		if !errors.Is(err, ErrRuntimeNotFound) || got != (Definition{}) {
			t.Fatalf("Resolve(%q) = %+v, %v", name, got, err)
		}
	}
}

func TestCatalogValidation(t *testing.T) {
	valid := DefinitionConfig{Name: "remote", Provider: ProviderWire, Endpoint: "tcp://127.0.0.1:9000"}
	for _, test := range []struct {
		name string
		edit func(*Config)
	}{
		{"empty name", func(c *Config) { c.Runtimes[0].Name = "" }},
		{"whitespace name", func(c *Config) { c.Runtimes[0].Name = " remote" }},
		{"reserved name", func(c *Config) { c.Runtimes[0].Name = "local" }},
		{"duplicate name", func(c *Config) { c.Runtimes = append(c.Runtimes, valid) }},
		{"unknown provider", func(c *Config) { c.Runtimes[0].Provider = "other" }},
		{"configured native", func(c *Config) { c.Runtimes[0].Provider = ProviderNative }},
		{"missing endpoint", func(c *Config) { c.Runtimes[0].Endpoint = "" }},
		{"blank endpoint", func(c *Config) { c.Runtimes[0].Endpoint = " \t" }},
		{"invalid timeout", func(c *Config) { c.Runtimes[0].ConnectTimeout = new("bad") }},
		{"empty timeout", func(c *Config) { c.Runtimes[0].ConnectTimeout = new("") }},
		{"zero timeout", func(c *Config) { c.Runtimes[0].ConnectTimeout = new("0s") }},
		{"negative timeout", func(c *Config) { c.Runtimes[0].ConnectTimeout = new("-1s") }},
		{"overflow timeout", func(c *Config) { c.Runtimes[0].ConnectTimeout = new("999999999999h") }},
		{"blank default", func(c *Config) { c.DefaultRuntime = new("") }},
		{"whitespace default", func(c *Config) { c.DefaultRuntime = new(" ") }},
		{"unknown default", func(c *Config) { c.DefaultRuntime = new("missing") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Config{Runtimes: []DefinitionConfig{valid}}
			test.edit(&config)

			catalog, err := NewCatalog(config)
			if !errors.Is(err, ErrInvalidConfig) || catalog != nil {
				t.Fatalf("NewCatalog = %v, %v", catalog, err)
			}

			if test.name == "unknown default" && !errors.Is(err, ErrRuntimeNotFound) {
				t.Fatalf("unknown default identity = %v", err)
			}
		})
	}
}

func BenchmarkNewCatalog(b *testing.B) {
	config := Config{Runtimes: []DefinitionConfig{{Name: "remote", Provider: ProviderWire, Endpoint: "tcp://127.0.0.1:9000"}}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := NewCatalog(config); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCatalogList(b *testing.B) {
	catalog, err := NewCatalog(Config{})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = catalog.List()
	}
}
