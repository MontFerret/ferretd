package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/MontFerret/ferretd/client"
	"github.com/MontFerret/ferretd/internal/transport"
)

func TestAuthenticatedTCPDaemonReportsAssignedEndpoint(t *testing.T) {
	requested, err := transport.ParseEndpoint("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	const token = "test-token-that-must-not-be-logged"
	diagnostics := newReadyDiagnosticWriter()
	logger := zerolog.New(diagnostics)
	d, err := New(Options{
		Version:     "test-version",
		Endpoint:    requested,
		BearerToken: token,
		Logger:      &logger,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	startCtx, cancelStart := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancelStart()
		stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
		defer cancelStop()
		_ = d.Stop(stopCtx)
	})

	startDone := make(chan error, 1)
	go func() {
		startDone <- d.Start(startCtx)
	}()

	var record []byte
	select {
	case record = <-diagnostics.records:
	case <-time.After(time.Second):
		t.Fatal("daemon did not report readiness")
	}

	if bytes.Contains(record, []byte(token)) {
		t.Fatal("ready diagnostic contains the bearer token")
	}

	var ready struct {
		Event    string `json:"event"`
		Endpoint string `json:"endpoint"`
		Version  string `json:"version"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(record), &ready); err != nil {
		t.Fatalf("decode ready diagnostic %q: %v", record, err)
	}
	if ready.Event != "ferretd.ready" || ready.Version != "test-version" || ready.Message != "ferretd started" {
		t.Fatalf("ready diagnostic = %#v", ready)
	}

	bound, err := client.ParseEndpoint(ready.Endpoint)
	if err != nil {
		t.Fatalf("ParseEndpoint reported endpoint: %v", err)
	}
	if bound.String() == requested.String() {
		t.Fatalf("reported endpoint = %q, want assigned port", bound.String())
	}

	for _, option := range []client.Option{nil, client.WithBearerToken("wrong-token")} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		options := []client.Option{client.WithEndpoint(bound)}
		if option != nil {
			options = append(options, option)
		}
		_, dialErr := client.Dial(ctx, options...)
		cancel()
		if status.Code(dialErr) != codes.Unauthenticated {
			t.Fatalf("Dial with option %#v error = %v, want Unauthenticated", option, dialErr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	connection, err := client.Dial(ctx, client.WithEndpoint(bound), client.WithBearerToken(token))
	cancel()
	if err != nil {
		t.Fatalf("authenticated Dial: %v", err)
	}

	infoCtx, cancelInfo := context.WithTimeout(context.Background(), time.Second)
	info, err := connection.Info(infoCtx)
	cancelInfo()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Version != "test-version" || info.APIVersion != (client.APIVersion{Major: 1, Minor: 2}) {
		t.Fatalf("Info = %#v", info)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	if err := connection.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		t.Fatalf("Shutdown: %v", err)
	}
	cancelShutdown()
	_ = connection.Close()

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after Shutdown")
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := d.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestDaemonOptionsRequireAuthenticationOnlyForTCP(t *testing.T) {
	tcpEndpoint, err := transport.ParseEndpoint("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	if _, err := New(Options{Endpoint: tcpEndpoint}); err == nil {
		t.Fatal("New accepted unauthenticated TCP")
	}

	if _, err := New(Options{Endpoint: testEndpoint(t), BearerToken: "unexpected"}); err == nil {
		t.Fatal("New accepted bearer authentication for a native endpoint")
	}
}
