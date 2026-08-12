//go:build windows

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/client"
)

func testClientEndpoint(t *testing.T) client.Endpoint {
	t.Helper()

	return client.Endpoint{
		Network: "npipe",
		Address: fmt.Sprintf(`\\.\pipe\ferretd-test-%d`, time.Now().UnixNano()),
	}
}

func assertEndpointRemoved(t *testing.T, endpoint client.Endpoint) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	connection, err := client.Dial(ctx, client.WithEndpoint(endpoint))
	if err == nil {
		_ = connection.Close()
		t.Fatal("endpoint remains available after shutdown")
	}
}
