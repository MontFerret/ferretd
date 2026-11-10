package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/client"
	"github.com/MontFerret/ferretd/internal/daemon"
	"github.com/MontFerret/ferretd/internal/runtimes"
	"github.com/MontFerret/ferretd/internal/transport"
)

type daemonFixture struct {
	ready            chan string
	allowStopFailure bool
	connection       *client.Client
}

func newDaemonFixture(t testing.TB, config runtimes.Config) *daemonFixture {
	t.Helper()
	f := &daemonFixture{ready: make(chan string, 1)}
	logger := zerolog.New(f)

	d, err := daemon.New(daemon.Options{Endpoint: transport.Endpoint{Network: transport.NetworkTCP, Address: "127.0.0.1:0"}, BearerToken: "test-token", RuntimeConfig: config, Logger: &logger})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- d.Start(context.Background()) }()
	t.Cleanup(func() {
		if f.connection != nil {
			_ = f.connection.Close()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := d.Stop(ctx); err != nil && !f.allowStopFailure {
			t.Error(err)
		}

		select {
		case <-done:
		case <-ctx.Done():
			t.Error("daemon shutdown did not settle")
		}
	})
	var endpoint string
	select {
	case endpoint = <-f.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not become ready")
	}

	parsed, err := client.ParseEndpoint(endpoint)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	f.connection, err = client.Dial(ctx, client.WithEndpoint(parsed), client.WithBearerToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}

	return f
}

func (f *daemonFixture) Write(data []byte) (int, error) {
	var record struct {
		Event    string `json:"event"`
		Endpoint string `json:"endpoint"`
	}
	if json.Unmarshal(data, &record) == nil && record.Event == "ferretd.ready" {
		f.ready <- record.Endpoint
	}

	return len(data), nil
}
