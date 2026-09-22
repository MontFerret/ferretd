package dap

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2/uapi"
)

func TestDAPInitialStop(t *testing.T) {
	const programText = "LET value = 1\nRETURN value\n"

	for _, test := range []struct {
		name        string
		text        string
		stopOnEntry bool
		breakpoints []protocol.SourceBreakpoint
		readd       bool
		reason      string
		line        int
		column      int
	}{
		{name: "suppressed entry", text: programText},
		{name: "breakpoint instead of suppressed entry", text: programText, breakpoints: []protocol.SourceBreakpoint{{Line: 1}}, reason: "breakpoint", line: 1, column: 1},
		{name: "entry", text: programText, stopOnEntry: true, reason: "entry", line: 1, column: 1},
		{name: "breakpoint takes priority over entry", text: programText, stopOnEntry: true, breakpoints: []protocol.SourceBreakpoint{{Line: 1}}, reason: "breakpoint", line: 1, column: 1},
		{name: "relocated entry breakpoint", text: "\n\n" + programText, breakpoints: []protocol.SourceBreakpoint{{Line: 1}}, reason: "breakpoint", line: 3, column: 1},
		{name: "multiple entry identities", text: programText, breakpoints: []protocol.SourceBreakpoint{{Line: 1}, {Line: 1, Column: 1}, {Line: 1}}, reason: "breakpoint", line: 1, column: 1},
		{name: "readded identity", text: programText, breakpoints: []protocol.SourceBreakpoint{{Line: 1}}, readd: true, reason: "breakpoint", line: 1, column: 1},
		{name: "unbound breakpoint", text: programText, breakpoints: []protocol.SourceBreakpoint{{Line: 3}}},
		{name: "later statement", text: programText, breakpoints: []protocol.SourceBreakpoint{{Line: 2}}, reason: "breakpoint", line: 2, column: 1},
		{name: "later statement on same line", text: "LET value = 1 RETURN value\n", breakpoints: []protocol.SourceBreakpoint{{Line: 1, Column: 15}}, reason: "breakpoint", line: 1, column: 15},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t)
			root := t.TempDir()
			program := writeDAPProgram(t, root, test.text)
			initializeDAP(t, client)
			launchDAP(t, client, program, root, test.stopOnEntry)
			bound := replaceInitialBreakpoints(t, client, program, test.breakpoints...)

			if test.readd {
				replaceInitialBreakpoints(t, client, program)
				readded := replaceInitialBreakpoints(t, client, program, test.breakpoints...)

				if readded[0].Id != bound[0].Id {
					t.Fatalf("readded ID = %d, want %d", readded[0].Id, bound[0].Id)
				}
			}

			var wantIDs []int

			for _, breakpoint := range bound {
				if breakpoint.Verified != (test.reason == "breakpoint") {
					t.Fatalf("breakpoint verification = %+v", breakpoint)
				}

				if breakpoint.Verified {
					wantIDs = append(wantIDs, breakpoint.Id)
				}
			}

			slices.Sort(wantIDs)
			wantIDs = slices.Compact(wantIDs)
			configureInitialDAP(t, client)

			if test.reason != "" {
				stopped, ok := client.read().(*protocol.StoppedEvent)
				if !ok || stopped.Body.Reason != test.reason || !slices.Equal(stopped.Body.HitBreakpointIds, wantIDs) {
					t.Fatalf("initial stop = %#v, want %s with IDs %v", stopped, test.reason, wantIDs)
				}

				client.send(&protocol.StackTraceRequest{
					Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID},
				})

				stack, ok := client.read().(*protocol.StackTraceResponse)
				if !ok || !stack.Success || len(stack.Body.StackFrames) != 1 {
					t.Fatalf("stack = %#v", stack)
				}

				frame := stack.Body.StackFrames[0]
				if frame.Source == nil || frame.Source.Path != program || frame.Line != test.line || frame.Column != test.column {
					t.Fatalf("frame = %+v, want %s:%d:%d", frame, program, test.line, test.column)
				}

				continueDAP(t, client)
			}

			// Reading the entire ordered transcript through termination proves there
			// was exactly one visible stop (or zero for suppressed entry).
			readInitialDAPCompletion(t, client)
			client.disconnect()
		})
	}
}

func TestDAPInitialStopPreservesStartTimeBindingsDuringReplacement(t *testing.T) {
	for _, test := range []struct {
		name        string
		stopOnEntry bool
		initial     bool
		next        []protocol.SourceBreakpoint
	}{
		{name: "clear committed breakpoint", initial: true},
		{name: "replace committed breakpoint", initial: true, next: []protocol.SourceBreakpoint{{Line: 2}}},
		{name: "replace with entry enabled", stopOnEntry: true, initial: true, next: []protocol.SourceBreakpoint{{Line: 2}}},
		{name: "add after suppressed entry committed", next: []protocol.SourceBreakpoint{{Line: 1}, {Line: 2}}},
		{name: "add after visible entry committed", stopOnEntry: true, next: []protocol.SourceBreakpoint{{Line: 1}, {Line: 2}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(cancel)

			native, err := uapi.New()
			if err != nil {
				t.Fatal(err)
			}

			captured, release := make(chan *apidebugger.Event, 1), make(chan struct{})
			d := &breakpointDebugger{}
			d.startFn = func(runCtx context.Context) (*apidebugger.Event, error) {
				event, startErr := d.Session.Start(runCtx)
				captured <- event

				select {
				case <-release:
					return event, startErr
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-runCtx.Done():
					return nil, runCtx.Err()
				}
			}
			client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: d})
			root := t.TempDir()
			program := writeDAPProgram(t, root, "LET value = 1\nRETURN value\n")
			initializeDAP(t, client)
			launchDAP(t, client, program, root, test.stopOnEntry)
			var initialID int

			if test.initial {
				initialID = replaceInitialBreakpoints(t, client, program, protocol.SourceBreakpoint{Line: 1})[0].Id
			}

			configureInitialDAP(t, client)

			select {
			case event := <-captured:
				if event == nil || event.Reason != apidebugger.ReasonEntry || len(event.HitBreakpointIDs) != 0 {
					t.Fatalf("native initial event = %+v", event)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			// Several edits can retire and reallocate native IDs before DAP sees
			// the captured stop. None may rewrite its configuration-time cause.
			replaceInitialBreakpoints(t, client, program)
			replaceInitialBreakpoints(t, client, program, protocol.SourceBreakpoint{Line: 1})
			replaced := replaceInitialBreakpoints(t, client, program, test.next...)

			canonical, err := d.Breakpoints(ctx)
			if err != nil || len(canonical) != len(test.next) {
				t.Fatalf("replacement was not applied immediately: %+v, %v", canonical, err)
			}

			for index, breakpoint := range canonical {
				if breakpoint.RequestedLocation.Line != test.next[index].Line {
					t.Fatalf("canonical breakpoint = %+v, want %+v", breakpoint, test.next[index])
				}
			}

			close(release)

			if test.initial || test.stopOnEntry {
				wantReason := stopReasonEntry
				var wantIDs []int

				if test.initial {
					wantReason = stopReasonBreakpoint
					wantIDs = []int{initialID}
				}

				stopped, ok := client.read().(*protocol.StoppedEvent)
				if !ok || stopped.Body.Reason != wantReason || !slices.Equal(stopped.Body.HitBreakpointIds, wantIDs) {
					t.Fatalf("committed stop = %#v, want %s with IDs %v", stopped, wantReason, wantIDs)
				}

				continueDAP(t, client)
			}

			if len(replaced) != 0 {
				wantID := replaced[len(replaced)-1].Id

				stopped, ok := client.read().(*protocol.StoppedEvent)
				if !ok || stopped.Body.Reason != stopReasonBreakpoint || !slices.Equal(stopped.Body.HitBreakpointIds, []int{wantID}) {
					t.Fatalf("subsequent stop = %#v, want breakpoint %d", stopped, wantID)
				}

				continueDAP(t, client)
			}

			readInitialDAPCompletion(t, client)
			client.disconnect()
		})
	}
}

func TestDAPInitialStopSurvivesFailedReplacement(t *testing.T) {
	native, err := uapi.New()
	if err != nil {
		t.Fatal(err)
	}

	d := &breakpointDebugger{}
	client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: d})
	root := t.TempDir()
	program := writeDAPProgram(t, root, "LET value = 1\nRETURN value\n")
	initializeDAP(t, client)
	launchDAP(t, client, program, root, false)
	wantID := replaceInitialBreakpoints(t, client, program, protocol.SourceBreakpoint{Line: 1})[0].Id
	rejected := errors.New("replacement rejected")
	d.replaceFn = func(context.Context, string, []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
		return nil, rejected
	}
	client.send(&protocol.SetBreakpointsRequest{
		Request: client.request("setBreakpoints"),
		Arguments: protocol.SetBreakpointsArguments{
			Source: protocol.Source{Path: program}, Breakpoints: []protocol.SourceBreakpoint{{Line: 2}},
		},
	})

	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success || response.Message != rejected.Error() {
		t.Fatalf("failed replacement = %#v", response)
	}

	configureInitialDAP(t, client)

	stopped, ok := client.read().(*protocol.StoppedEvent)
	if !ok || stopped.Body.Reason != stopReasonBreakpoint || !slices.Equal(stopped.Body.HitBreakpointIds, []int{wantID}) {
		t.Fatalf("preserved initial breakpoint = %#v", stopped)
	}

	continueDAP(t, client)
	readInitialDAPCompletion(t, client)
	client.disconnect()
}

func replaceInitialBreakpoints(t *testing.T, client *testClient, program string, breakpoints ...protocol.SourceBreakpoint) []protocol.Breakpoint {
	t.Helper()
	client.send(&protocol.SetBreakpointsRequest{
		Request: client.request("setBreakpoints"),
		Arguments: protocol.SetBreakpointsArguments{
			Source: protocol.Source{Path: program}, Breakpoints: breakpoints,
		},
	})

	response, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !response.Success || len(response.Body.Breakpoints) != len(breakpoints) {
		t.Fatalf("setBreakpoints = %#v", response)
	}

	return response.Body.Breakpoints
}

func configureInitialDAP(t *testing.T, client *testClient) {
	t.Helper()
	client.send(&protocol.ConfigurationDoneRequest{Request: client.request("configurationDone")})

	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone = %#v", response)
	}

	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch = %#v", response)
	}
}

func readInitialDAPCompletion(t *testing.T, client *testClient) {
	t.Helper()

	if output, ok := client.read().(*protocol.OutputEvent); !ok || output.Body.Category != outputCategoryStdout || output.Body.Output != "1\n" {
		t.Fatalf("completion output = %#v", output)
	}

	if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 0 {
		t.Fatalf("exit = %#v", exited)
	}

	if terminated, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatalf("termination = %#v", terminated)
	}
}
