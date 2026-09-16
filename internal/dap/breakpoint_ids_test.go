package dap

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"

	protocol "github.com/google/go-dap"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/debug"
)

func TestDAPOlderQueuedStopPreservesNewerCommandHits(t *testing.T) {
	native, err := uapi.New()
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer

	server, err := newServer(strings.NewReader(""), &output, Options{}.normalized(), native)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = server.cleanup() })
	ids := server.breakpoints
	requests := []apidebugger.BreakpointRequest{{Position: apisource.Position{Line: 2}}}
	ids.replace("program", requests, []apidebugger.Breakpoint{{ID: 10}})
	stable := ids.id(10)
	ids.commandStarted()
	// The first command stopped upstream, but its DAP event is still queued.
	ids.replace("program", requests, []apidebugger.Breakpoint{{ID: 11}})
	ids.commandStarted()
	ids.replace("program", nil, nil)

	for index, hit := range []apidebugger.BreakpointID{10, 11} {
		server.handleDebugEvent(debug.Event{Snapshot: debug.SessionSnapshot{
			State: debug.StateStopped, Reason: apidebugger.ReasonBreakpoint,
			HitBreakpointIDs: []apidebugger.BreakpointID{hit},
		}})

		message, err := protocol.ReadProtocolMessage(bufio.NewReader(&output))
		if err != nil {
			t.Fatal(err)
		}

		stopped, ok := message.(*protocol.StoppedEvent)
		if !ok || len(stopped.Body.HitBreakpointIds) != 1 || stopped.Body.HitBreakpointIds[0] != stable {
			t.Fatalf("queued stop = %#v", message)
		}

		if index == 0 && ids.id(11) != stable {
			t.Fatal("older stop discarded the newer command's hit identity")
		}
	}

	if len(ids.active) != 0 || len(ids.retired) != 0 || ids.pending != 0 {
		t.Fatalf("completed commands retained native identities: %+v", ids)
	}

	// Churn while stopped must not retain native tombstones, and stable requested
	// identities survive both native reallocation and resolved-location changes.
	for nativeID := apidebugger.BreakpointID(12); nativeID < 1000; nativeID++ {
		ids.replace("program", requests, []apidebugger.Breakpoint{{ID: nativeID, Location: apisource.Range{Location: apisource.Location{Position: apisource.Position{Line: 3}}}}})

		if ids.id(nativeID) != stable {
			t.Fatal("requested identity changed on relocation")
		}

		ids.replace("program", nil, nil)

		if len(ids.active) != 0 || len(ids.retired) != 0 {
			t.Fatal("stopped replacement retained removed native identities")
		}
	}
}

func TestDAPFailedReplacementPreservesIdentitiesAndNativeSet(t *testing.T) {
	native, err := uapi.New()
	if err != nil {
		t.Fatal(err)
	}

	failure := errors.New("replacement rejected")
	d := &breakpointDebugger{}
	reject := false
	d.replaceFn = func(ctx context.Context, source string, requests []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
		if reject {
			return nil, failure
		}

		return d.Session.ReplaceBreakpoints(ctx, source, requests)
	}
	client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: d})
	root := t.TempDir()
	f := &liveBreakpointFixture{t: t, client: client, program: writeDAPProgram(t, root, "LET x = 1\nRETURN x + 1")}
	initializeDAP(t, client)
	launchDAP(t, client, f.program, root, true)
	f.replace(2)
	client.server.eventMu.Lock()
	ids := client.server.breakpoints
	stable, active, retired, next := maps.Clone(ids.stable), maps.Clone(ids.active), maps.Clone(ids.retired), ids.next
	client.server.eventMu.Unlock()

	before, err := d.Breakpoints(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	reject = true
	client.send(f.replacement(1))

	response, ok := client.read().(*protocol.ErrorResponse)
	if !ok || response.Success || response.Message != failure.Error() {
		t.Fatalf("failed replacement = %#v", response)
	}

	client.server.eventMu.Lock()
	if !maps.Equal(ids.stable, stable) || !maps.Equal(ids.active, active) || !maps.Equal(ids.retired, retired) || ids.next != next {
		t.Error("failed replacement published DAP identities")
	}

	client.server.eventMu.Unlock()

	after, err := d.Breakpoints(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("native set changed: %+v, %v", after, err)
	}

	completePendingDAPLaunch(t, client)
	client.disconnect()
}
