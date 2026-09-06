package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestParseLoopbackTCPEndpoint(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "tcp://127.0.0.1:0", want: "tcp://127.0.0.1:0"},
		{value: "tcp://127.0.0.1:65535", want: "tcp://127.0.0.1:65535"},
		{value: "tcp://localhost:0", want: "invalid"},
		{value: "tcp://127.0.0.2:0", want: "invalid"},
		{value: "tcp://[::1]:0", want: "invalid"},
		{value: "tcp://127.0.0.1", want: "invalid"},
		{value: "tcp://127.0.0.1:0/path", want: "invalid"},
		{value: "tcp://127.0.0.1:0?query=1", want: "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			endpoint, err := ParseEndpoint(tt.value)
			if tt.want == "invalid" {
				if !errors.Is(err, ErrInvalidEndpoint) {
					t.Fatalf("ParseEndpoint error = %v, want ErrInvalidEndpoint", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}

			if got := endpoint.String(); got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListenAssignsAndDialsEphemeralTCPPort(t *testing.T) {
	requested, err := ParseEndpoint("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}

	listener, err := Listen(requested)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	bound := listener.Endpoint()
	if bound == requested || bound.Network != NetworkTCP {
		t.Fatalf("bound endpoint = %#v, want assigned TCP endpoint", bound)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	connection, err := Dial(ctx, bound)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	_ = connection.Close()

	serverConnection := <-accepted
	if serverConnection != nil {
		_ = serverConnection.Close()
	}
}

func TestTCPListenAndDialRejectWrongPortRole(t *testing.T) {
	assigned, err := ParseEndpoint("tcp://127.0.0.1:12345")
	if err != nil {
		t.Fatalf("ParseEndpoint assigned: %v", err)
	}

	if _, err := Listen(assigned); !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("Listen error = %v, want ErrInvalidEndpoint", err)
	}

	unassigned, err := ParseEndpoint("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("ParseEndpoint unassigned: %v", err)
	}

	if _, err := Dial(context.Background(), unassigned); !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("Dial error = %v, want ErrInvalidEndpoint", err)
	}
}
