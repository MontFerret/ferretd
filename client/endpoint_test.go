package client

import (
	"context"
	"errors"
	"testing"
)

func TestParseEndpointRejectsMalformedOrUnsupportedURI(t *testing.T) {
	for _, value := range []string{
		"unix:///%zz",
		"tcp://127.0.0.1:50051",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseEndpoint(value)
			if !errors.Is(err, ErrInvalidEndpoint) {
				t.Fatalf("ParseEndpoint error = %v, want ErrInvalidEndpoint", err)
			}
		})
	}
}

func TestDialRejectsZeroEndpoint(t *testing.T) {
	_, err := Dial(context.Background(), WithEndpoint(Endpoint{}))
	if !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("Dial error = %v, want ErrInvalidEndpoint", err)
	}
}
