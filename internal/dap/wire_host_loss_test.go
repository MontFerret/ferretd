package dap

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/runtimes"
	wireserver "github.com/MontFerret/wire/server"
)

func TestDAPSuspendedWireHostLoss(t *testing.T) {
	client, stopHost := wireDebugClient(t, &breakpointDebugger{})
	root := t.TempDir()
	program := writeDAPProgram(t, root, "LET value = 1\nRETURN value\n")
	initializeDAP(t, client)
	launchDAP(t, client, program, root, false)
	replaceInitialBreakpoints(t, client, program, protocol.SourceBreakpoint{Line: 2})
	configureInitialDAP(t, client)

	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != stopReasonBreakpoint {
		t.Fatalf("breakpoint = %#v", stopped)
	}

	// DAP stdin stays open until its failure, exit and termination are received.
	stopHost()
	readWireFailure(t, client)
}

func TestDAPWireLossOvertakesSuppressedEntry(t *testing.T) {
	release := make(chan struct{})
	dbg := &breakpointDebugger{}
	dbg.startFn = func(ctx context.Context) (*apidebugger.Event, error) {
		select {
		case <-release:
			return dbg.Session.Start(ctx)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	client, stopHost := wireDebugClient(t, dbg)
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 42")
	initializeDAP(t, client)
	launchDAP(t, client, program, root, false)
	id := client.server.owned.debug

	watch, err := client.server.debugs.WatchSession(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}

	defer watch.Cancel()

	configureInitialDAP(t, client)
	// Hold DAP presentation while the domain commits both entry and loss.
	// When released, the adapter processes a stale entry before the failure.
	func() {
		client.server.eventMu.Lock()
		defer client.server.eventMu.Unlock()
		close(release)
		for _, want := range []debug.EventKind{debug.EventRunning, debug.EventStopped, debug.EventFailed} {
			select {
			case event := <-watch.Events:
				if event.Kind != want {
					t.Fatalf("debug event = %v, want %v", event.Kind, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("timed out waiting for event %v", want)
			}

			if want == debug.EventStopped {
				stopHost()
			}
		}
	}()

	readWireFailure(t, client)
}

func wireDebugClient(t *testing.T, dbg *breakpointDebugger) (*testClient, func()) {
	t.Helper()

	host, err := uapi.New("test")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})

	server, err := wireserver.New(&breakpointRuntime{Runtime: host, debugger: dbg})
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(t.Context(), listener) }()
	stopHost := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}

	t.Cleanup(func() {
		stopHost()
		select {
		case err := <-serveDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Wire server did not exit")
		}
	})

	config := runtimes.Config{
		DefaultRuntime: new("App"),
		Runtimes: []runtimes.DefinitionConfig{{
			Name: "App", Provider: runtimes.ProviderWire, Endpoint: "tcp://" + listener.Addr().String(), Insecure: true,
		}},
	}

	return newTestClientWithOptions(t, Options{RuntimeConfig: config}), stopHost
}

func readWireFailure(t *testing.T, client *testClient) {
	t.Helper()

	output, ok := client.read().(*protocol.OutputEvent)
	if !ok || output.Body.Category != outputCategoryStderr || !strings.Contains(output.Body.Output, `Wire runtime "App"`) {
		t.Fatalf("failure diagnostic = %#v", output)
	}

	if exit, ok := client.read().(*protocol.ExitedEvent); !ok || exit.Body.ExitCode != 1 {
		t.Fatalf("exit = %#v", exit)
	}

	if event, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatalf("termination = %#v", event)
	}

	request := client.request("disconnect")
	client.send(&protocol.DisconnectRequest{Request: request})

	if response, ok := client.read().(*protocol.DisconnectResponse); !ok || !response.Success {
		t.Fatalf("message after terminated = %#v", response)
	}
}
