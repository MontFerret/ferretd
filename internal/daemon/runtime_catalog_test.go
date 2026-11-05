package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MontFerret/api"
	supportedclient "github.com/MontFerret/ferretd/client"
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
			d, err := bootstrap(Options{RuntimeConfig: test.config}, func() (api.Runtime, error) {
				t.Fatal("allocated runtime before configuration validation")

				return nil, nil
			})
			if d != nil || !errors.Is(err, test.want) {
				t.Fatalf("bootstrap = %v, %v", d, err)
			}
		})
	}
}

func TestRuntimeDiscoveryAndNativeExecution(t *testing.T) {
	// A control connection queued after discovery proves no earlier connection
	// attempt reached this listener, without relying on a negative timeout.
	outbound, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = outbound.Close() })

	unavailable, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	unavailableAddress := unavailable.Addr().String()
	if err := unavailable.Close(); err != nil {
		t.Fatal(err)
	}

	endpoint := testEndpoint(t)
	config := runtimes.Config{Runtimes: []runtimes.DefinitionConfig{
		{Name: "unavailable", Provider: runtimes.ProviderWire, Endpoint: "tcp://" + unavailableAddress},
		{Name: "observed", Label: "Observed target", Provider: runtimes.ProviderWire, Endpoint: "tcp://" + outbound.Addr().String()},
	}}

	d, err := New(Options{Endpoint: endpoint, RuntimeConfig: config})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- d.Start(context.Background()) }()
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := d.Stop(cleanupContext); err != nil {
			t.Error(err)
		}

		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-cleanupContext.Done():
			t.Error("daemon did not stop during cleanup")
		}
	})
	waitForEndpoint(t, endpoint)

	publicEndpoint, err := supportedclient.ParseEndpoint(endpoint.String())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	c, err := supportedclient.Dial(ctx, supportedclient.WithEndpoint(publicEndpoint))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = c.Close() })

	discovery, err := c.ListRuntimes(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if discovery.DefaultRuntime != "local" || len(discovery.Runtimes) != 3 ||
		discovery.Runtimes[0].Name != "local" || discovery.Runtimes[1].Name != "observed" ||
		discovery.Runtimes[1].Label != "Observed target" || discovery.Runtimes[2].Provider != "wire" {
		t.Fatalf("discovery = %+v", discovery)
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "query.fql"), []byte("RETURN 42"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspace, err := c.Workspaces().Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	session, err := c.Executions().CreateSession(ctx, supportedclient.CreateSessionRequest{WorkspaceID: workspace.ID, RelativePath: "query.fql"})
	if err != nil {
		t.Fatal(err)
	}

	execution, err := c.Executions().CreateExecution(ctx, supportedclient.CreateExecutionRequest{SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}

	watch, err := c.Executions().WatchExecution(ctx, execution.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Executions().RunExecution(ctx, execution.ID); err != nil {
		t.Fatal(err)
	}

	for {
		event, err := watch.Recv()
		if err != nil {
			t.Fatal(err)
		}

		if event.Kind == supportedclient.ExecutionEventCompleted {
			if event.Execution.Output == nil || string(event.Execution.Output.Data) != "42" {
				t.Fatalf("output = %+v", event.Execution.Output)
			}

			break
		}
	}

	control, err := (&net.Dialer{}).DialContext(ctx, "tcp", outbound.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = control.Close() }()

	if err := outbound.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	accepted, err := outbound.Accept()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = accepted.Close() }()

	if accepted.RemoteAddr().String() != control.LocalAddr().String() {
		t.Fatal("runtime configuration or discovery connected to the Wire target")
	}
}
