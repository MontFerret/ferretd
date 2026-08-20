//go:build windows

package daemon

import (
	"fmt"
	"testing"
	"time"

	"github.com/MontFerret/ferretd/internal/transport"
)

func testEndpoint(t testing.TB) transport.Endpoint {
	t.Helper()

	return transport.Endpoint{
		Network: "npipe",
		Address: fmt.Sprintf(`\\.\pipe\ferretd-test-%d`, time.Now().UnixNano()),
	}
}
