//go:build !windows

package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestDialClassifiesUnavailableDaemon(t *testing.T) {
	endpoint := Endpoint{
		Network: "unix",
		Address: filepath.Join("/tmp", fmt.Sprintf("ferretd-missing-%d.sock", time.Now().UnixNano())),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := Dial(ctx, WithEndpoint(endpoint))
	if !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("Dial error = %v, want ErrDaemonUnavailable", err)
	}
}
