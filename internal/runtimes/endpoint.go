package runtimes

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

func wireTarget(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("expected tcp://host:port")
	}

	if parsed.Scheme != "tcp" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Path != "" || strings.ContainsAny(endpoint, "?#") {
		return "", errors.New("expected tcp://host:port without userinfo, path, query, or fragment")
	}

	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || host == "" || port == "" {
		return "", errors.New("expected a hostname or IP address and numeric port")
	}

	if strings.HasPrefix(parsed.Host, "[") {
		address, err := netip.ParseAddr(host)
		if err != nil || !address.Is6() {
			return "", errors.New("brackets require an IPv6 address")
		}
	}

	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return "", errors.New("port must be numeric in 1..65535")
		}
	}

	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", errors.New("port must be numeric in 1..65535")
	}

	return net.JoinHostPort(host, strconv.FormatUint(number, 10)), nil
}
