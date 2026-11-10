package integration_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2/uapi"
	wireclient "github.com/MontFerret/wire/client"
	wireserver "github.com/MontFerret/wire/server"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

func newWireOwner(t testing.TB, definitions ...runtimes.DefinitionConfig) *runtimes.Owner {
	t.Helper()

	catalog, err := runtimes.NewCatalog(runtimes.Config{Runtimes: definitions})
	if err != nil {
		t.Fatal(err)
	}

	native, err := uapi.New(runtimes.NativeVersion)
	if err != nil {
		t.Fatal(err)
	}

	owner, err := runtimes.NewOwner(catalog, native)
	if err != nil {
		_ = native.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Tests check the committed result explicitly, including transport failures.
		_ = owner.Close(ctx)
	})

	return owner
}

func assertApplicationResult(t *testing.T, runtime api.Runtime, want int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	plan, err := runtime.Compile(ctx, api.NewAnonymousSource("RETURN TASK3_APPLICATION_VALUE() + @offset"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := plan.Close(); err != nil {
			t.Error(err)
		}
	}()

	params, err := plan.Params(ctx)
	if err != nil || len(params) != 1 || params[0] != "offset" {
		t.Fatalf("Plan.Params = %v, %v", params, err)
	}

	session, err := plan.NewSession(ctx, api.WithParams(map[string]any{"offset": 1}))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()

	output, err := session.Run(ctx)
	if output != nil {
		// Collect settles admitted output; explicit Close also covers a rejected
		// collection or an early test failure before consumption takes ownership.
		defer func() {
			if err := output.Close(); err != nil {
				t.Error(err)
			}
		}()
	}

	if err != nil || output == nil {
		t.Fatalf("Session.Run = %v, %v", output, err)
	}

	content, err := output.Collect(ctx)
	if err != nil || content == nil || string(content.Data) != strconv.Itoa(want) {
		t.Fatalf("Collect = %+v, %v; want %d", content, err, want)
	}
}

func TestWireAcquisitionApplicationAndIndependentLifetimes(t *testing.T) {
	fixture := newWireFixture(t, 41)
	owner := newWireOwner(t, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: fixture.endpoint, Insecure: true})
	startupCtx, cancel := context.WithCancel(t.Context())
	defer cancel()

	first, err := owner.Acquire(startupCtx, "application")
	if err != nil {
		t.Fatal(err)
	}

	firstPeer := fixture.connection(t)
	cancel()
	assertApplicationResult(t, first.Runtime(), 42)

	second, err := owner.Acquire(t.Context(), "application")
	if err != nil {
		t.Fatal(err)
	}

	if secondPeer := fixture.connection(t); secondPeer == firstPeer {
		t.Fatal("same-target acquisitions shared a physical channel")
	}

	assertApplicationResult(t, second.Runtime(), 42)

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}

	assertApplicationResult(t, second.Runtime(), 42)

	if err := owner.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := second.Release(); err != nil {
		t.Fatal(err)
	}

	if fixture.closes.Load() != 0 {
		t.Fatal("client cleanup closed the application's hosted runtime")
	}

	assertApplicationResult(t, fixture.runtime, 42)
	fixture.closeServer(t)

	if fixture.closes.Load() != 0 {
		t.Fatal("Wire server closed its borrowed hosted runtime")
	}

	assertApplicationResult(t, fixture.runtime, 42)
}

func TestWirePlaintextRequiresExplicitInsecure(t *testing.T) {
	fixture := newWireFixture(t, 41)
	owner := newWireOwner(t,
		runtimes.DefinitionConfig{Name: "tls-default", Provider: runtimes.ProviderWire, Endpoint: fixture.endpoint, ConnectTimeout: new("1s")},
		runtimes.DefinitionConfig{Name: "plaintext", Provider: runtimes.ProviderWire, Endpoint: fixture.endpoint, Insecure: true},
	)

	if binding, err := owner.Acquire(t.Context(), "tls-default"); binding != nil || err == nil {
		t.Fatalf("TLS acquisition against plaintext = %v, %v", binding, err)
	}

	binding, err := owner.Acquire(t.Context(), "plaintext")
	if err != nil {
		t.Fatal(err)
	}

	assertApplicationResult(t, binding.Runtime(), 42)

	if err := binding.Release(); err != nil {
		t.Fatal(err)
	}

	if err := owner.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestWireFailedHandshakeAllowsFreshAcquisition(t *testing.T) {
	var rejected atomic.Bool
	fixture := newWireFixture(t, 41, wireserver.WithStreamInterceptors(func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if strings.HasSuffix(info.FullMethod, "/Connect") && rejected.CompareAndSwap(false, true) {
			return status.Error(codes.FailedPrecondition, "application rejected handshake")
		}

		return handler(srv, stream)
	}))
	owner := newWireOwner(t, runtimes.DefinitionConfig{Name: "application", Provider: runtimes.ProviderWire, Endpoint: fixture.endpoint, Insecure: true})

	binding, err := owner.Acquire(t.Context(), "application")

	var wireErr *wireclient.Error
	if binding != nil || !errors.As(err, &wireErr) || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("failed handshake = %v, %v", binding, err)
	}

	binding, err = owner.Acquire(t.Context(), "application")
	if err != nil {
		t.Fatal(err)
	}

	assertApplicationResult(t, binding.Runtime(), 42)

	if err := binding.Release(); err != nil {
		t.Fatal(err)
	}

	if err := owner.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestWireConnectionLossAndExplicitReplacement(t *testing.T) {
	first := newWireFixture(t, 41)
	replacement := newWireFixture(t, 99)
	owner := newWireOwner(t,
		runtimes.DefinitionConfig{Name: "original", Provider: runtimes.ProviderWire, Endpoint: first.endpoint, Insecure: true},
		runtimes.DefinitionConfig{Name: "replacement", Provider: runtimes.ProviderWire, Endpoint: replacement.endpoint, Insecure: true},
	)

	original, err := owner.Acquire(t.Context(), "original")
	if err != nil {
		t.Fatal(err)
	}

	first.connection(t)
	assertApplicationResult(t, original.Runtime(), 42)
	first.closeServer(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	plan, err := original.Runtime().Compile(ctx, api.NewAnonymousSource("RETURN 1"))
	if plan != nil {
		_ = plan.Close()
	}

	if err == nil {
		t.Fatal("original binding survived definitive connection loss")
	}

	fresh, err := owner.Acquire(ctx, "replacement")
	if err != nil {
		t.Fatal(err)
	}

	replacement.connection(t)
	assertApplicationResult(t, fresh.Runtime(), 100)

	plan, err = original.Runtime().Compile(ctx, api.NewAnonymousSource("RETURN 1"))
	if plan != nil {
		_ = plan.Close()
	}

	if err == nil {
		t.Fatal("explicit replacement revived the original binding")
	}

	if err := fresh.Release(); err != nil {
		t.Fatal(err)
	}

	// Wire may retain the definitive transport failure as the release result.
	releaseErr := original.Release()

	closeErr := owner.Close(ctx)
	if releaseErr == nil && closeErr != nil || releaseErr != nil && !errors.Is(closeErr, releaseErr) {
		t.Fatalf("owner lost release result: release = %v; owner = %v", releaseErr, closeErr)
	}
}
