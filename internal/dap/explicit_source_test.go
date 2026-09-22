package dap

import (
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/google/go-dap"
)

func TestDAPLaunchExplicitExcludedSource(t *testing.T) {
	root := t.TempDir()

	program := filepath.Join(root, ".tmp", "test.fql")
	if err := os.Mkdir(filepath.Dir(program), 0o700); err != nil {
		t.Fatal(err)
	}

	original := "LET value = 41\nRETURN value + 1"
	if err := os.WriteFile(program, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	breakpoints := replaceInitialBreakpoints(t, client, program, protocol.SourceBreakpoint{Line: 2})
	if len(breakpoints) != 1 || !breakpoints[0].Verified || breakpoints[0].Source == nil || breakpoints[0].Source.Path != program {
		t.Fatalf("explicit source breakpoint = %#v", breakpoints)
	}

	completePendingDAPLaunch(t, client)

	// Disk edits cannot replace the launched normal/debug Plan or its source identity.
	if err := os.WriteFile(program, []byte("RETURN 999"), 0o600); err != nil {
		t.Fatal(err)
	}

	continueDAP(t, client)

	stopped, ok := client.read().(*protocol.StoppedEvent)
	if !ok || stopped.Body.Reason != "breakpoint" || len(stopped.Body.HitBreakpointIds) != 1 || stopped.Body.HitBreakpointIds[0] != breakpoints[0].Id {
		t.Fatalf("breakpoint stop = %#v", stopped)
	}

	client.send(&protocol.StackTraceRequest{Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID}})

	stack, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || !stack.Success || len(stack.Body.StackFrames) != 1 {
		t.Fatalf("stack = %#v", stack)
	}

	frame := stack.Body.StackFrames[0]
	if frame.Source == nil || frame.Source.Path != program || frame.Line != 2 {
		t.Fatalf("selected source frame = %#v", frame)
	}

	client.send(&protocol.ScopesRequest{Request: client.request("scopes"), Arguments: protocol.ScopesArguments{FrameId: frame.Id}})

	scopes, ok := client.read().(*protocol.ScopesResponse)
	if !ok || !scopes.Success || len(scopes.Body.Scopes) != 2 {
		t.Fatalf("scopes = %#v", scopes)
	}

	client.send(&protocol.VariablesRequest{Request: client.request("variables"), Arguments: protocol.VariablesArguments{VariablesReference: scopes.Body.Scopes[0].VariablesReference}})

	variables, ok := client.read().(*protocol.VariablesResponse)
	if !ok || !variables.Success || !protocolVariablesContain(variables.Body.Variables, "value", "41") {
		t.Fatalf("variables = %#v", variables)
	}

	client.send(&protocol.EvaluateRequest{Request: client.request("evaluate"), Arguments: protocol.EvaluateArguments{FrameId: frame.Id, Expression: "value + 1"}})

	evaluated, ok := client.read().(*protocol.EvaluateResponse)
	if !ok || !evaluated.Success || evaluated.Body.Result != "42" {
		t.Fatalf("evaluation = %#v", evaluated)
	}

	client.server.stateMu.Lock()
	owned := client.server.owned
	client.server.stateMu.Unlock()

	snapshot, err := client.server.executions.GetSession(t.Context(), owned.session)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Source.RelativePath != ".tmp/test.fql" || snapshot.Text != original {
		t.Fatalf("compiled snapshot = %+v", snapshot)
	}

	continueDAP(t, client)

	output, ok := client.read().(*protocol.OutputEvent)
	if !ok || output.Body.Output != "42\n" {
		t.Fatalf("output = %#v", output)
	}

	if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 0 {
		t.Fatalf("exit = %#v", exited)
	}

	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("expected termination")
	}

	client.disconnect()
}
