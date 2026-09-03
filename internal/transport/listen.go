package transport

import (
	"errors"
	"net"
	"sync"
)

// Listener owns a bound local endpoint.
type Listener struct {
	net.Listener
	endpoint Endpoint
	once     sync.Once
	err      error
}

// Listen creates a local listener for the endpoint.
func Listen(endpoint Endpoint) (*Listener, error) {
	if endpoint.Network == NetworkTCP {
		return listenTCP(endpoint)
	}

	listener, err := listenLocal(endpoint)
	if err != nil {
		return nil, err
	}

	return &Listener{Listener: listener, endpoint: endpoint}, nil
}

// Endpoint returns the concrete endpoint bound by the listener.
func (l *Listener) Endpoint() Endpoint {
	return l.endpoint
}

// Close releases the listener once and preserves its result for later callers.
func (l *Listener) Close() error {
	l.once.Do(func() {
		l.err = l.Listener.Close()
		if errors.Is(l.err, net.ErrClosed) {
			l.err = nil
		}
	})

	return l.err
}
