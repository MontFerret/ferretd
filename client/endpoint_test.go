package client

import (
	"context"
	"errors"
	"testing"
)

func TestParseEndpointRejectsMalformedOrUnsupportedURI(t *testing.T) {
	for _, value := range []string{
		"unix:///%zz",
		"tcp://localhost:50051",
		"tcp://127.0.0.1",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseEndpoint(value)
			if !errors.Is(err, ErrInvalidEndpoint) {
				t.Fatalf("ParseEndpoint error = %v, want ErrInvalidEndpoint", err)
			}
		})
	}
}

func TestParseEndpointAcceptsLoopbackTCP(t *testing.T) {
	endpoint, err := ParseEndpoint("tcp://127.0.0.1:50051")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	if got, want := endpoint.String(), "tcp://127.0.0.1:50051"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestDialRejectsZeroEndpoint(t *testing.T) {
	_, err := Dial(context.Background(), WithEndpoint(Endpoint{}))
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("Dial error = %v, want ErrInvalidEndpoint", err)
	}
}

func TestDialRejectsEmptyBearerToken(t *testing.T) {
	_, err := Dial(context.Background(), WithBearerToken(""))
	if !errors.Is(err, ErrInvalidBearerToken) {
		t.Fatalf("Dial error = %v, want ErrInvalidBearerToken", err)
	}
}
