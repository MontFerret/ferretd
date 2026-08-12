//go:build !windows

package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestDialClassifiesUnavailableDaemon(t *testing.T) {
	endpoint, err := ParseEndpoint((&url.URL{
		Scheme: "unix",
		Path:   filepath.Join("/tmp", fmt.Sprintf("ferretd-missing-%d.sock", time.Now().UnixNano())),
	}).String())
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

func TestParseUnixEndpoint(t *testing.T) {
	endpoint, err := ParseEndpoint("unix:///tmp/ferret/../ferretd.sock")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	if got, want := endpoint.String(), "unix:///tmp/ferretd.sock"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}

	roundTrip, err := ParseEndpoint(endpoint.String())
	if err != nil {
		t.Fatalf("round-trip ParseEndpoint: %v", err)
	}

	if roundTrip != endpoint {
		t.Fatalf("round-trip endpoint = %#v, want %#v", roundTrip, endpoint)
	}

	_, err = ParseEndpoint("npipe:////./pipe/ferretd")
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("ParseEndpoint named pipe error = %v, want ErrInvalidEndpoint", err)
	}
}
