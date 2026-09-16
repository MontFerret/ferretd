package dap

import (
	"context"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferret/v2"
	ferruntime "github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/uapi"
)

type liveBreakpointFixture struct {
	t       *testing.T
	client  *testClient
	program string
	entered chan struct{}
	release chan struct{}
}

func newLiveBreakpointFixture(t *testing.T, stopOnEntry bool, initial ...int) *liveBreakpointFixture {
	t.Helper()
	f := &liveBreakpointFixture{t: t, entered: make(chan struct{}), release: make(chan struct{})}

	runtime, err := uapi.New(ferret.WithFunctionsRegistrar(func(ns ferruntime.Namespace) {
		ns.Function().A0().Add("GATE", func(ctx context.Context) (ferruntime.Value, error) {
			select {
			case f.entered <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			select {
			case <-f.release:
				return ferruntime.NewInt(1), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	}))
	if err != nil {
		t.Fatal(err)
	}

	f.client = newTestClientWithRuntime(t, Options{}, runtime)
	root := t.TempDir()
	f.program = writeDAPProgram(t, root, "RETURN FOR i IN 1..3\n LET gate = GATE()\n\n LET x = i + gate\n LET y = x + 1\n RETURN y")
	initializeDAP(t, f.client)
	launchDAP(t, f.client, f.program, root, stopOnEntry)
	f.replace(initial...)

	if stopOnEntry {
		completePendingDAPLaunch(t, f.client)
	} else {
		f.client.send(&protocol.ConfigurationDoneRequest{Request: f.client.request("configurationDone")})

		if response, ok := f.client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
			t.Fatalf("configuration = %#v", response)
		}

		if response, ok := f.client.read().(*protocol.LaunchResponse); !ok || !response.Success {
			t.Fatalf("launch = %#v", response)
		}
	}

	return f
}

func (f *liveBreakpointFixture) replacement(lines ...int) *protocol.SetBreakpointsRequest {
	breakpoints := make([]protocol.SourceBreakpoint, len(lines))
	for index, line := range lines {
		breakpoints[index] = protocol.SourceBreakpoint{Line: line}
	}

	return &protocol.SetBreakpointsRequest{
		Request: f.client.request("setBreakpoints"),
		Arguments: protocol.SetBreakpointsArguments{
			Source: protocol.Source{Path: f.program}, Breakpoints: breakpoints,
		},
	}
}

func (f *liveBreakpointFixture) replace(lines ...int) []protocol.Breakpoint {
	f.t.Helper()
	f.client.send(f.replacement(lines...))

	response, ok := f.client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !response.Success || len(response.Body.Breakpoints) != len(lines) {
		f.t.Fatalf("replacement = %#v", response)
	}

	return response.Body.Breakpoints
}

func (f *liveBreakpointFixture) continueExecution() {
	f.t.Helper()
	f.client.send(&protocol.ContinueRequest{Request: f.client.request("continue"), Arguments: protocol.ContinueArguments{ThreadId: threadID}})

	if response, ok := f.client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		f.t.Fatalf("continue = %#v", response)
	}
}

func (f *liveBreakpointFixture) waitVisit() {
	f.t.Helper()

	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		f.t.Fatal("execution did not reach the next gated visit")
	}
}

func (f *liveBreakpointFixture) advance() {
	f.t.Helper()

	select {
	case f.release <- struct{}{}:
	case <-time.After(5 * time.Second):
		f.t.Fatal("execution did not leave the gated visit")
	}
}

func (f *liveBreakpointFixture) stopped(reason string, id int) {
	f.t.Helper()

	stopped, ok := f.client.read().(*protocol.StoppedEvent)
	if !ok || stopped.Body.Reason != reason {
		f.t.Fatalf("stop = %#v, want %s", stopped, reason)
	}

	if id != 0 && (len(stopped.Body.HitBreakpointIds) != 1 || stopped.Body.HitBreakpointIds[0] != id) {
		f.t.Fatalf("hit = %+v, want %d", stopped.Body.HitBreakpointIds, id)
	}
}

func (f *liveBreakpointFixture) assertMappings(active, retired, pending int) {
	f.t.Helper()
	f.client.server.eventMu.Lock()
	defer f.client.server.eventMu.Unlock()

	ids := f.client.server.breakpoints
	if len(ids.active) != active || len(ids.retired) != retired || ids.pending != pending {
		f.t.Fatalf("mappings: active %d, retired %d, pending %d; want %d, %d, %d", len(ids.active), len(ids.retired), ids.pending, active, retired, pending)
	}
}
