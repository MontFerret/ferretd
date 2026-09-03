// Package transport provides local endpoint discovery, listening, and dialing.
package transport

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type (
	// Network identifies a supported local transport family.
	Network string

	// Endpoint identifies a local daemon transport.
	Endpoint struct {
		Network Network
		Address string
	}
)

const (
	// NetworkUnix identifies a local Unix-domain socket endpoint.
	NetworkUnix Network = "unix"
	// NetworkNamedPipe identifies a local Windows named-pipe endpoint.
	NetworkNamedPipe Network = "npipe"
	// NetworkTCP identifies an IPv4 loopback TCP endpoint.
	NetworkTCP Network = "tcp"
)

// ParseEndpoint parses a supported local endpoint URL for the current platform.
func ParseEndpoint(value string) (Endpoint, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: parse URL: %v", ErrInvalidEndpoint, err)
	}

	switch Network(parsed.Scheme) {
	case NetworkUnix:
		if runtime.GOOS == "windows" {
			return Endpoint{}, fmt.Errorf("%w: unix endpoints are not supported on Windows", ErrInvalidEndpoint)
		}

		if parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !filepath.IsAbs(parsed.Path) {
			return Endpoint{}, fmt.Errorf("%w: unix endpoint must contain only an absolute path", ErrInvalidEndpoint)
		}

		return Endpoint{Network: NetworkUnix, Address: filepath.Clean(parsed.Path)}, nil
	case NetworkNamedPipe:
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

		return Endpoint{Network: NetworkNamedPipe, Address: address}, nil
	case NetworkTCP:
		if parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Endpoint{}, fmt.Errorf("%w: TCP endpoint contains unsupported URL components", ErrInvalidEndpoint)
		}

		host, portValue, err := net.SplitHostPort(parsed.Host)
		if err != nil {
			return Endpoint{}, fmt.Errorf("%w: TCP endpoint must contain an IPv4 loopback host and port", ErrInvalidEndpoint)
		}

		if host != "127.0.0.1" {
			return Endpoint{}, fmt.Errorf("%w: TCP endpoint must use 127.0.0.1", ErrInvalidEndpoint)
		}

		port, err := strconv.ParseUint(portValue, 10, 16)
		if err != nil {
			return Endpoint{}, fmt.Errorf("%w: TCP endpoint contains an invalid port", ErrInvalidEndpoint)
		}

		return Endpoint{
			Network: NetworkTCP,
			Address: net.JoinHostPort(host, strconv.FormatUint(port, 10)),
		}, nil
	default:
		return Endpoint{}, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidEndpoint, parsed.Scheme)
	}
}

// String returns the endpoint's URL form.
func (e Endpoint) String() string {
	switch e.Network {
	case NetworkUnix:
		return (&url.URL{Scheme: string(e.Network), Path: e.Address}).String()
	case NetworkNamedPipe:
		return (&url.URL{
			Scheme: string(e.Network),
			Path:   strings.ReplaceAll(e.Address, `\`, "/"),
		}).String()
	case NetworkTCP:
		return (&url.URL{Scheme: string(e.Network), Host: e.Address}).String()
	default:
		return ""
	}
}
