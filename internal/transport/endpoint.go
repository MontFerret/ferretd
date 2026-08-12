// Package transport provides local endpoint discovery, listening, and dialing.
package transport

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// Endpoint identifies a local daemon transport.
type Endpoint struct {
	Network string
	Address string
}

// ParseEndpoint parses a supported local endpoint URL for the current platform.
func ParseEndpoint(value string) (Endpoint, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: parse URL: %v", ErrInvalidEndpoint, err)
	}

	switch parsed.Scheme {
	case "unix":
		if runtime.GOOS == "windows" {
			return Endpoint{}, fmt.Errorf("%w: unix endpoints are not supported on Windows", ErrInvalidEndpoint)
		}

		if parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !filepath.IsAbs(parsed.Path) {
			return Endpoint{}, fmt.Errorf("%w: unix endpoint must contain only an absolute path", ErrInvalidEndpoint)
		}

		return Endpoint{Network: "unix", Address: filepath.Clean(parsed.Path)}, nil
	case "npipe":
		if runtime.GOOS != "windows" {
			return Endpoint{}, fmt.Errorf("%w: named-pipe endpoints are only supported on Windows", ErrInvalidEndpoint)
		}

		if parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Endpoint{}, fmt.Errorf("%w: named-pipe endpoint contains unsupported URL components", ErrInvalidEndpoint)
		}

		address := strings.ReplaceAll(parsed.Path, "/", `\`)
		if !strings.HasPrefix(strings.ToLower(address), `\\.\pipe\`) || len(address) == len(`\\.\pipe\`) {
			return Endpoint{}, fmt.Errorf("%w: named-pipe endpoint must be under \\.\\pipe", ErrInvalidEndpoint)
		}

		return Endpoint{Network: "npipe", Address: address}, nil
	default:
		return Endpoint{}, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidEndpoint, parsed.Scheme)
	}
}

// String returns the endpoint's URL form.
func (e Endpoint) String() string {
	switch e.Network {
	case "unix":
		return (&url.URL{Scheme: "unix", Path: e.Address}).String()
	case "npipe":
		return (&url.URL{
			Scheme: "npipe",
			Path:   strings.ReplaceAll(e.Address, `\`, "/"),
		}).String()
	default:
		return ""
	}
}
