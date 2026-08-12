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
	endpoint := Endpoint{
		Network: "npipe",
		Address: fmt.Sprintf(`\\.\pipe\ferretd-missing-%d`, time.Now().UnixNano()),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := Dial(ctx, WithEndpoint(endpoint))
	if !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("Dial error = %v, want ErrDaemonUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Dial error = %v, want context.DeadlineExceeded", err)
	}
}
