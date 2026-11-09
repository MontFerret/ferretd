package integration_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"

	"github.com/MontFerret/ferret/v2"
	ferruntime "github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/uapi"
	wireserver "github.com/MontFerret/wire/server"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

type wireFixture struct {
	runtime     *uapi.Runtime
	server      *wireserver.Server
	endpoint    string
	connections chan string
	serving     chan error
	cancel      context.CancelFunc
	stop        sync.Once
	stopErr     error
	closes      atomic.Int32
}

func newWireFixture(t *testing.T, applicationValue int, options ...wireserver.Option) *wireFixture {
	t.Helper()
	f := &wireFixture{connections: make(chan string, 16), serving: make(chan error, 1)}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	f.endpoint = "tcp://" + listener.Addr().String()

	f.runtime, err = uapi.New(runtimes.NativeVersion,
		ferret.WithFunctionsRegistrar(func(ns ferruntime.Namespace) {
			ns.Function().A0().Add("TASK3_APPLICATION_VALUE", func(context.Context) (ferruntime.Value, error) {
				return ferruntime.NewInt(applicationValue), nil
			})
		}),
		ferret.WithEngineCloseHook(func() error {
			f.closes.Add(1)

			return nil
		}),
	)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}

	options = append(options, wireserver.WithStreamInterceptors(f.observeConnection))

	f.server, err = wireserver.New(f.runtime, options...)
	if err != nil {
		_ = listener.Close()
		_ = f.runtime.Close()
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	go func() { f.serving <- f.server.Serve(ctx, listener) }()
	t.Cleanup(func() {
		f.closeServer(t)

		if err := f.runtime.Close(); err != nil {
			t.Error(err)
		}
	})

	return f
}

func (f *wireFixture) observeConnection(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if strings.HasSuffix(info.FullMethod, "/Connect") {
		if remote, ok := peer.FromContext(stream.Context()); ok {
			f.connections <- remote.Addr.String()
		}
	}

	return handler(srv, stream)
}

func (f *wireFixture) closeServer(t *testing.T) {
	t.Helper()
	f.stop.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defer f.cancel()

		f.stopErr = f.server.Shutdown(ctx)
		select {
		case err := <-f.serving:
			f.stopErr = errors.Join(f.stopErr, err)
		case <-ctx.Done():
			f.stopErr = errors.Join(f.stopErr, ctx.Err())
		}
	})

	if f.stopErr != nil {
		t.Errorf("Wire server shutdown: %v", f.stopErr)
	}
}

func (f *wireFixture) connection(t *testing.T) string {
	t.Helper()

	select {
	case address := <-f.connections:
		return address
	case <-time.After(5 * time.Second):
		t.Fatal("Wire handshake was not observed")

		return ""
	}
}
