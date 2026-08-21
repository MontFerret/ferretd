//go:build windows

package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestParseEndpoint(t *testing.T) {
	endpoint, err := ParseEndpoint("npipe:////./pipe/ferretd-test")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	if got, want := endpoint.Address, `\\.\pipe\ferretd-test`; got != want {
		t.Fatalf("address = %q, want %q", got, want)
	}
	if got, want := endpoint.String(), "npipe:////./pipe/ferretd-test"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}

	for _, value := range []string{"unix:///tmp/ferretd.sock", "tcp://127.0.0.1:50051"} {
		if _, err := ParseEndpoint(value); !errors.Is(err, ErrInvalidEndpoint) {
			t.Fatalf("ParseEndpoint(%q) error = %v, want ErrInvalidEndpoint", value, err)
		}
	}
}

func TestListenDial(t *testing.T) {
	endpoint := Endpoint{
		Network: NetworkNamedPipe,
		Address: fmt.Sprintf(`\\.\pipe\ferretd-test-%d`, time.Now().UnixNano()),
	}
	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	connection, err := Dial(ctx, endpoint)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = connection.Close()

	serverConnection := <-accepted
	if serverConnection != nil {
		_ = serverConnection.Close()
	}
}

func TestListenRefusesActiveEndpoint(t *testing.T) {
	endpoint := Endpoint{
		Network: NetworkNamedPipe,
		Address: fmt.Sprintf(`\\.\pipe\ferretd-test-%d`, time.Now().UnixNano()),
	}
	listener, err := Listen(endpoint)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	_, err = Listen(endpoint)
	if !errors.Is(err, ErrEndpointInUse) {
		t.Fatalf("second Listen error = %v, want ErrEndpointInUse", err)
	}
}
