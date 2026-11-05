package dap

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferretd/internal/runtimes"
)

func mustNewRuntimeCatalog(t testing.TB) *runtimes.Catalog {
	t.Helper()

	catalog, err := runtimes.NewCatalog(runtimes.Config{})
	if err != nil {
		t.Fatal(err)
	}

	return catalog
}

func TestRuntimeConfigurationPrecedesAllocation(t *testing.T) {
	for _, test := range []struct {
		name   string
		config runtimes.Config
		want   error
	}{
		{"invalid", runtimes.Config{DefaultRuntime: new("missing")}, runtimes.ErrInvalidConfig},
		{"wire default", runtimes.Config{DefaultRuntime: new("app"), Runtimes: []runtimes.DefinitionConfig{{Name: "app", Provider: runtimes.ProviderWire, Endpoint: "unparsed"}}}, runtimes.ErrUnsupportedDefault},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, err := bootstrap(strings.NewReader(""), io.Discard, Options{RuntimeConfig: test.config}, func() (api.Runtime, error) {
				t.Fatal("allocated runtime before configuration validation")

				return nil, nil
			})
			if server != nil || !errors.Is(err, test.want) {
				t.Fatalf("bootstrap = %v, %v", server, err)
			}
		})
	}
}

func TestRuntimeConfigurationAcceptsUnusedWire(t *testing.T) {
	server, err := New(strings.NewReader(""), io.Discard, Options{RuntimeConfig: runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "app", Provider: runtimes.ProviderWire, Endpoint: "unparsed"}}}})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := server.cleanup(); err != nil {
			t.Error(err)
		}
	})

	if server.catalog.DefaultName() != "local" || len(server.catalog.List()) != 2 {
		t.Fatalf("catalog = %+v", server.catalog.List())
	}
}
