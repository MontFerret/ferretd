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

func TestDialOptionsValidateAuthenticationForResolvedEndpoint(t *testing.T) {
	tcpEndpoint, err := ParseEndpoint("tcp://127.0.0.1:50051")
	if err != nil {
		t.Fatalf("ParseEndpoint TCP: %v", err)
	}

	nativeEndpoint, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	tests := []struct {
		name         string
		options      []Option
		wantEndpoint string
		wantErr      error
	}{
		{
			name:         "TCP with bearer token",
			options:      []Option{WithEndpoint(tcpEndpoint), WithBearerToken("token")},
			wantEndpoint: tcpEndpoint.String(),
		},
		{
			name:    "TCP without bearer token",
			options: []Option{WithEndpoint(tcpEndpoint)},
			wantErr: ErrBearerTokenRequired,
		},
		{
			name:         "native without bearer token",
			options:      []Option{WithEndpoint(nativeEndpoint)},
			wantEndpoint: nativeEndpoint.String(),
		},
		{
			name:    "native with bearer token",
			options: []Option{WithEndpoint(nativeEndpoint), WithBearerToken("token")},
			wantErr: ErrBearerTokenUnsupported,
		},
		{
			name:         "default endpoint",
			wantEndpoint: nativeEndpoint.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configuration := dialOptions{}
			for _, option := range tt.options {
				if err := option(&configuration); err != nil {
					t.Fatalf("apply option: %v", err)
				}
			}

			endpoint, err := configuration.resolvedEndpoint()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("resolvedEndpoint error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("resolvedEndpoint: %v", err)
			}

			if got := endpoint.String(); got != tt.wantEndpoint {
				t.Fatalf("endpoint = %q, want %q", got, tt.wantEndpoint)
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

func TestDialRejectsEmptyBearerToken(t *testing.T) {
	_, err := Dial(context.Background(), WithBearerToken(""))
	if !errors.Is(err, ErrInvalidBearerToken) {
		t.Fatalf("Dial error = %v, want ErrInvalidBearerToken", err)
	}
}
