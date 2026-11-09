package dap

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

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
		{"invalid endpoint", runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "app", Provider: runtimes.ProviderWire, Endpoint: "tcp://localhost:0"}}}, runtimes.ErrInvalidConfig},
		{"wire default", runtimes.Config{DefaultRuntime: new("app"), Runtimes: []runtimes.DefinitionConfig{{Name: "app", Provider: runtimes.ProviderWire, Endpoint: "tcp://application.invalid:9000"}}}, runtimes.ErrUnsupportedDefault},
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
	outbound, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = outbound.Close() })

	server, err := New(strings.NewReader(""), io.Discard, Options{RuntimeConfig: runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "app", Provider: runtimes.ProviderWire, Endpoint: "tcp://" + outbound.Addr().String()}}}})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := server.cleanup(); err != nil {
			t.Error(err)
		}
	})

	version, err := server.runtimeBinding.Runtime().Version(t.Context())
	if err != nil || version != runtimes.NativeVersion {
		t.Fatalf("native runtime version = %q, %v", version, err)
	}

	if server.catalog.DefaultName() != "local" || len(server.catalog.List()) != 2 {
		t.Fatalf("catalog = %+v", server.catalog.List())
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	control, err := (&net.Dialer{}).DialContext(ctx, "tcp", outbound.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = control.Close() }()

	if err := outbound.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	accepted, err := outbound.Accept()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = accepted.Close() }()

	if accepted.RemoteAddr().String() != control.LocalAddr().String() {
		t.Fatal("DAP startup connected to an unused Wire target")
	}
}
