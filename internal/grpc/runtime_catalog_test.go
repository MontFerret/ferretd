package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"
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

func TestRuntimeDiscoverySnapshot(t *testing.T) {
	catalog, err := runtimes.NewCatalog(runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: "tcp://application.invalid:9000"}}})
	if err != nil {
		t.Fatal(err)
	}

	service, err := newDaemonService("test", "instance", func() {}, catalog)
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.ListRuntimes(t.Context(), &daemonv1.ListRuntimesRequest{})
	if err != nil || response.DefaultRuntime != "local" || len(response.Runtimes) != 2 || response.Runtimes[0].Name != "application" || response.Runtimes[0].Provider != "wire" {
		t.Fatalf("ListRuntimes = %+v, %v", response, err)
	}

	response.Runtimes[0].Name = "changed"

	next, err := service.ListRuntimes(t.Context(), &daemonv1.ListRuntimesRequest{})
	if err != nil || next.Runtimes[0].Name != "application" {
		t.Fatalf("snapshot = %+v, %v", next, err)
	}

	fields := next.Runtimes[0].ProtoReflect().Descriptor().Fields()
	if fields.Len() != 3 {
		t.Fatal("discovery includes connection configuration")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := service.ListRuntimes(ctx, &daemonv1.ListRuntimesRequest{}); status.Code(err) != codes.Canceled {
		t.Fatalf("canceled discovery = %v", err)
	}
}
