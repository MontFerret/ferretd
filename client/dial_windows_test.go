//go:build windows

package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDialClassifiesUnavailableDaemon(t *testing.T) {
	endpoint, err := ParseEndpoint(fmt.Sprintf("npipe:////./pipe/ferretd-missing-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = Dial(ctx, WithEndpoint(endpoint))
	if !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("Dial error = %v, want ErrDaemonUnavailable", err)
	}
}

func TestParseNamedPipeEndpoint(t *testing.T) {
	endpoint, err := ParseEndpoint("npipe:////./pipe/ferretd")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	if got, want := endpoint.String(), "npipe:////./pipe/ferretd"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}

	roundTrip, err := ParseEndpoint(endpoint.String())
	if err != nil {
		t.Fatalf("round-trip ParseEndpoint: %v", err)
	}

	if roundTrip != endpoint {
		t.Fatalf("round-trip endpoint = %#v, want %#v", roundTrip, endpoint)
	}

	_, err = ParseEndpoint("unix:///tmp/ferretd.sock")
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("ParseEndpoint Unix socket error = %v, want ErrInvalidEndpoint", err)
	}
}
