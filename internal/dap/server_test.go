package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/source"
)

type testClient struct {
	t        *testing.T
	input    *io.PipeWriter
	output   *bufio.Reader
	cancel   context.CancelFunc
	done     <-chan error
	server   *Server
	sequence int
}

func mustNewServer(t testing.TB, input io.Reader, output io.Writer) *Server {
	t.Helper()

	server, err := New(input, output)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return server
}

func TestNewRequiresStreams(t *testing.T) {
	tests := []struct {
		name   string
		input  io.Reader
		output io.Writer
	}{
		{name: "nil input", output: io.Discard},
		{name: "nil output", input: strings.NewReader("")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := New(test.input, test.output)
			if err == nil {
				t.Fatal("New error = nil, want non-nil")
			}
			if server != nil {
				t.Fatalf("New server = %v, want nil", server)
			}
		})
	}
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()

	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	server := mustNewServer(t, serverInput, serverOutput)
	done := make(chan error, 1)
	go func() {
		err := server.Run(ctx)
		_ = serverInput.Close()
		_ = serverOutput.Close()
		done <- err
	}()

	client := &testClient{
		t:      t,
		input:  clientInput,
		output: bufio.NewReader(clientOutput),
		cancel: cancel,
		done:   done,
		server: server,
	}
	t.Cleanup(func() {
		cancel()
		_ = clientInput.Close()
		_ = clientOutput.Close()
	})

	return client
}

func (c *testClient) request(command string) protocol.Request {
	c.t.Helper()
	c.sequence++

	return protocol.Request{
		ProtocolMessage: protocol.ProtocolMessage{Seq: c.sequence, Type: "request"},
		Command:         command,
	}
}

func (c *testClient) send(message protocol.Message) {
	c.t.Helper()

	if err := protocol.WriteProtocolMessage(c.input, message); err != nil {
		c.t.Fatalf("WriteProtocolMessage: %v", err)
	}
}

func (c *testClient) sendRawRequest(command, arguments string) protocol.Request {
	c.t.Helper()

	request := c.request(command)
	payload := fmt.Sprintf(
		`{"seq":%d,"type":"request","command":%q,"arguments":%s}`,
		request.Seq,
		command,
		arguments,
	)
	if err := protocol.WriteBaseMessage(c.input, []byte(payload)); err != nil {
		c.t.Fatalf("WriteBaseMessage: %v", err)
	}

	return request
}

func (c *testClient) read() protocol.Message {
	c.t.Helper()

	result := make(chan struct {
		message protocol.Message
		err     error
	}, 1)
	go func() {
		message, err := protocol.ReadProtocolMessage(c.output)
		result <- struct {
			message protocol.Message
			err     error
		}{message: message, err: err}
	}()

	select {
	case got := <-result:
		if got.err != nil {
			c.t.Fatalf("ReadProtocolMessage: %v", got.err)
		}

		return got.message
	case <-time.After(5 * time.Second):
		c.t.Fatal("timed out reading DAP message")

		return nil
	}
}

func (c *testClient) disconnect() {
	c.t.Helper()

	request := c.request("disconnect")
	c.send(&protocol.DisconnectRequest{Request: request})
	if response, ok := c.read().(*protocol.DisconnectResponse); !ok || !response.Success {
		c.t.Fatalf("disconnect response = %#v", response)
	}

	select {
	case err := <-c.done:
		if err != nil {
			c.t.Fatalf("server Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		c.t.Fatal("server did not stop after disconnect")
	}
}

func waitForDAPStop(t *testing.T, client *testClient) {
	t.Helper()

	select {
	case err := <-client.done:
		if err != nil {
			t.Fatalf("server Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestDAPContextCancellationUnblocksIdleStream(t *testing.T) {
	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	t.Cleanup(func() {
		_ = clientInput.Close()
		_ = clientOutput.Close()
		_ = serverOutput.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	server := mustNewServer(t, serverInput, serverOutput)
	go func() {
		done <- server.Run(ctx)
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
}

func TestDAPContextCancellationUnblocksPendingLaunch(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	client.cancel()
	select {
	case err := <-client.done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop with a pending launch")
	}
}

func TestDAPInitializeClientOptionDefaultsAndConventions(t *testing.T) {
	tests := []struct {
		name        string
		arguments   string
		want        clientOptions
		wantFailure bool
	}{
		{
			name:      "omitted_defaults",
			arguments: `{"adapterID":"ferretd"}`,
			want: clientOptions{
				pathFormat:      "path",
				linesStartAt1:   true,
				columnsStartAt1: true,
			},
		},
		{
			name:      "explicit_one_based",
			arguments: `{"adapterID":"ferretd","linesStartAt1":true,"columnsStartAt1":true}`,
			want: clientOptions{
				pathFormat:      "path",
				linesStartAt1:   true,
				columnsStartAt1: true,
			},
		},
		{
			name:      "explicit_uri",
			arguments: `{"adapterID":"ferretd","pathFormat":"uri"}`,
			want: clientOptions{
				pathFormat:      "uri",
				linesStartAt1:   true,
				columnsStartAt1: true,
			},
		},
		{
			name:      "explicit_zero_based",
			arguments: `{"adapterID":"ferretd","pathFormat":"path","linesStartAt1":false,"columnsStartAt1":false}`,
			want: clientOptions{
				pathFormat:      "path",
				linesStartAt1:   false,
				columnsStartAt1: false,
			},
		},
		{
			name:      "mixed_coordinate_conventions",
			arguments: `{"adapterID":"ferretd","linesStartAt1":false}`,
			want: clientOptions{
				pathFormat:      "path",
				linesStartAt1:   false,
				columnsStartAt1: true,
			},
		},
		{
			name:      "defensive_empty_path_format",
			arguments: `{"adapterID":"ferretd","pathFormat":""}`,
			want: clientOptions{
				pathFormat:      "path",
				linesStartAt1:   true,
				columnsStartAt1: true,
			},
		},
		{
			name:        "unsupported_path_format",
			arguments:   `{"adapterID":"ferretd","pathFormat":"remote"}`,
			wantFailure: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t)
			client.sendRawRequest("initialize", test.arguments)
			message := client.read()
			if test.wantFailure {
				if response, ok := message.(*protocol.ErrorResponse); !ok || response.Success {
					t.Fatalf("initialize response = %#v", message)
				}
			} else {
				if response, ok := message.(*protocol.InitializeResponse); !ok || !response.Success {
					t.Fatalf("initialize response = %#v", message)
				}

				client.server.stateMu.Lock()
				got := client.server.client
				client.server.stateMu.Unlock()
				if got != test.want {
					t.Fatalf("client options = %+v, want %+v", got, test.want)
				}
			}

			client.disconnect()
		})
	}
}

func TestDAPLaunchConfigurationFramesScopesVariablesEvaluateAndCompletion(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, `LET box = {value: 10}
FUNC outer(p) {
  LET caller = p
  FUNC inner(q) {
    LET local = caller + q
    RETURN local
  }
  LET result = inner(3)
  RETURN result
}
RETURN outer(@input) + box.value`)
	typedProgramURI, err := source.URIFromPath(program)
	if err != nil {
		t.Fatal(err)
	}
	programURI := typedProgramURI.String()

	client := newTestClient(t)
	initialize := client.request("initialize")
	client.send(&protocol.InitializeRequest{
		Request: initialize,
		Arguments: protocol.InitializeRequestArguments{
			AdapterID:       "ferretd",
			LinesStartAt1:   false,
			ColumnsStartAt1: false,
			PathFormat:      "uri",
		},
	})
	initializeResponse, ok := client.read().(*protocol.InitializeResponse)
	if !ok || !initializeResponse.Success || !initializeResponse.Body.SupportsConfigurationDoneRequest ||
		!initializeResponse.Body.SupportsTerminateRequest || !initializeResponse.Body.SupportsEvaluateForHovers ||
		initializeResponse.Body.SupportsConditionalBreakpoints {
		t.Fatalf("initialize response = %#v", initializeResponse)
	}

	launchBody, err := json.Marshal(launchArguments{
		Program:     program,
		CWD:         root,
		Parameters:  map[string]any{"input": 2},
		StopOnEntry: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	launch := client.request("launch")
	client.send(&protocol.LaunchRequest{Request: launch, Arguments: launchBody})
	if _, ok := client.read().(*protocol.InitializedEvent); !ok {
		t.Fatal("expected initialized event")
	}
	client.server.stateMu.Lock()
	debugID := client.server.owned.debug
	client.server.stateMu.Unlock()
	debugSnapshot, err := client.server.debugs.GetSession(context.Background(), debugID)
	if err != nil || debugSnapshot.State != debug.StateCreated {
		t.Fatalf("debug Session before configurationDone = %+v, %v", debugSnapshot, err)
	}

	setBreakpoints := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: setBreakpoints,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: programURI},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 3}},
		},
	})
	breakpointResponse, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !breakpointResponse.Success || len(breakpointResponse.Body.Breakpoints) != 1 {
		t.Fatalf("setBreakpoints response = %#v", breakpointResponse)
	}
	breakpoint := breakpointResponse.Body.Breakpoints[0]
	if !breakpoint.Verified || breakpoint.Id == 0 || breakpoint.Line != 4 || breakpoint.Column < 0 || breakpoint.Source.Path != programURI {
		t.Fatalf("breakpoint = %#v", breakpoint)
	}
	replaceBreakpoints := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: replaceBreakpoints,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: programURI},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 3}},
		},
	})
	replacedResponse, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || len(replacedResponse.Body.Breakpoints) != 1 || replacedResponse.Body.Breakpoints[0].Id != breakpoint.Id {
		t.Fatalf("replacement breakpoint response = %#v", replacedResponse)
	}

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	message := client.read()
	if response, ok := message.(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", message)
	}
	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}
	entry, ok := client.read().(*protocol.StoppedEvent)
	if !ok || entry.Body.Reason != "entry" || entry.Body.ThreadId != threadID {
		t.Fatalf("entry event = %#v", entry)
	}

	threads := client.request("threads")
	client.send(&protocol.ThreadsRequest{Request: threads})
	threadResponse, ok := client.read().(*protocol.ThreadsResponse)
	if !ok || len(threadResponse.Body.Threads) != 1 || threadResponse.Body.Threads[0].Id != threadID {
		t.Fatalf("threads response = %#v", threadResponse)
	}

	continueRequest := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success || !response.Body.AllThreadsContinued {
		t.Fatalf("continue response = %#v", response)
	}
	breakpointEvent, ok := client.read().(*protocol.StoppedEvent)
	if !ok || breakpointEvent.Body.Reason != "breakpoint" || len(breakpointEvent.Body.HitBreakpointIds) != 1 ||
		breakpointEvent.Body.HitBreakpointIds[0] != breakpoint.Id {
		t.Fatalf("breakpoint event = %#v", breakpointEvent)
	}

	stackTrace := client.request("stackTrace")
	client.send(&protocol.StackTraceRequest{
		Request:   stackTrace,
		Arguments: protocol.StackTraceArguments{ThreadId: threadID},
	})
	stackResponse, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || len(stackResponse.Body.StackFrames) != 3 || stackResponse.Body.TotalFrames != 3 {
		t.Fatalf("stackTrace response = %#v", stackResponse)
	}
	if stackResponse.Body.StackFrames[0].Name != "inner" || stackResponse.Body.StackFrames[2].Source.Path != programURI {
		t.Fatalf("stack frames = %#v", stackResponse.Body.StackFrames)
	}

	scopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   scopes,
		Arguments: protocol.ScopesArguments{FrameId: stackResponse.Body.StackFrames[1].Id},
	})
	scopesResponse, ok := client.read().(*protocol.ScopesResponse)
	if !ok || len(scopesResponse.Body.Scopes) != 2 || scopesResponse.Body.Scopes[0].Name != "Locals" ||
		scopesResponse.Body.Scopes[1].Name != "Parameters" {
		t.Fatalf("scopes response = %#v", scopesResponse)
	}

	variables := client.request("variables")
	client.send(&protocol.VariablesRequest{
		Request: variables,
		Arguments: protocol.VariablesArguments{
			VariablesReference: scopesResponse.Body.Scopes[0].VariablesReference,
		},
	})
	variablesResponse, ok := client.read().(*protocol.VariablesResponse)
	if !ok || !protocolVariablesContain(variablesResponse.Body.Variables, "caller", "2") {
		t.Fatalf("variables response = %#v", variablesResponse)
	}

	evaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: evaluate,
		Arguments: protocol.EvaluateArguments{
			Expression: "caller + @input",
			FrameId:    stackResponse.Body.StackFrames[1].Id,
			Context:    "hover",
		},
	})
	evaluateResponse, ok := client.read().(*protocol.EvaluateResponse)
	if !ok || evaluateResponse.Body.Result != "4" || evaluateResponse.Body.Type != "Float" {
		t.Fatalf("evaluate response = %#v", evaluateResponse)
	}

	mainScopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   mainScopes,
		Arguments: protocol.ScopesArguments{FrameId: stackResponse.Body.StackFrames[2].Id},
	})
	mainScopesResponse, ok := client.read().(*protocol.ScopesResponse)
	if !ok || len(mainScopesResponse.Body.Scopes) != 2 {
		t.Fatalf("main scopes response = %#v", mainScopesResponse)
	}
	mainVariables := client.request("variables")
	client.send(&protocol.VariablesRequest{
		Request: mainVariables,
		Arguments: protocol.VariablesArguments{
			VariablesReference: mainScopesResponse.Body.Scopes[0].VariablesReference,
		},
	})
	mainVariablesResponse, ok := client.read().(*protocol.VariablesResponse)
	if !ok {
		t.Fatalf("main variables response = %#v", mainVariablesResponse)
	}
	boxReference := 0
	for _, variable := range mainVariablesResponse.Body.Variables {
		if variable.Name == "box" {
			boxReference = variable.VariablesReference
		}
	}
	if boxReference == 0 {
		t.Fatalf("box variable has no reference: %#v", mainVariablesResponse.Body.Variables)
	}
	boxVariables := client.request("variables")
	client.send(&protocol.VariablesRequest{
		Request: boxVariables,
		Arguments: protocol.VariablesArguments{
			VariablesReference: boxReference,
		},
	})
	boxVariablesResponse, ok := client.read().(*protocol.VariablesResponse)
	if !ok || !protocolVariablesContain(boxVariablesResponse.Body.Variables, "value", "10") {
		t.Fatalf("box variables response = %#v", boxVariablesResponse)
	}

	next := client.request("next")
	client.send(&protocol.NextRequest{
		Request:   next,
		Arguments: protocol.NextArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.NextResponse); !ok || !response.Success {
		t.Fatalf("next response = %#v", response)
	}
	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "step" {
		t.Fatalf("next stopped event = %#v", stopped)
	}

	staleScopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   staleScopes,
		Arguments: protocol.ScopesArguments{FrameId: stackResponse.Body.StackFrames[1].Id},
	})
	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success {
		t.Fatalf("stale scopes response = %#v", response)
	}

	continueRequest = client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("final continue response = %#v", response)
	}
	outputEvent, ok := client.read().(*protocol.OutputEvent)
	if !ok || outputEvent.Body.Category != "stdout" || outputEvent.Body.Output == "" {
		t.Fatalf("output event = %#v", outputEvent)
	}
	if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 0 {
		t.Fatalf("exited event = %#v", exited)
	}
	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("expected terminated event")
	}

	client.disconnect()
}

func TestDAPSuppressesEntryRuntimeErrorsAndUnsupportedRequests(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "LET x = 7\nRETURN x / 0")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, false)

	attach := client.request("attach")
	client.send(&protocol.AttachRequest{Request: attach, Arguments: json.RawMessage(`{}`)})
	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success {
		t.Fatalf("attach response = %#v", response)
	}

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", response)
	}
	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}
	runtimeError, ok := client.read().(*protocol.StoppedEvent)
	if !ok || runtimeError.Body.Reason != "exception" || runtimeError.Body.Description == "" {
		t.Fatalf("runtime error event = %#v", runtimeError)
	}

	continueRequest := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("continue response = %#v", response)
	}
	if output, ok := client.read().(*protocol.OutputEvent); !ok || output.Body.Category != "stderr" {
		t.Fatalf("failure output = %#v", output)
	}
	if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 1 {
		t.Fatalf("failure exit = %#v", exited)
	}
	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("expected failure terminated event")
	}

	client.disconnect()
}

func TestDAPRejectsOutOfOrderInitialization(t *testing.T) {
	client := newTestClient(t)

	launch := client.request("launch")
	client.send(&protocol.LaunchRequest{Request: launch, Arguments: json.RawMessage(`{"program":"query.fql"}`)})
	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success {
		t.Fatalf("launch-before-initialize response = %#v", response)
	}

	initializeDAP(t, client)
	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success {
		t.Fatalf("configurationDone-before-launch response = %#v", response)
	}

	client.disconnect()
}

func TestDAPConfigurationStartFailureResolvesPendingLaunch(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	client.server.stateMu.Lock()
	client.server.owned.debug = debug.SessionID("missing")
	client.server.stateMu.Unlock()

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	message := client.read()
	if response, ok := message.(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", message)
	}
	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success || response.Command != "launch" {
		t.Fatalf("pending launch response = %#v", response)
	}

	waitForDAPStop(t, client)
}

func TestDAPEarlyDisconnectAndTerminateResolvePendingLaunch(t *testing.T) {
	t.Run("disconnect", func(t *testing.T) {
		root := t.TempDir()
		program := writeDAPProgram(t, root, "RETURN 1")
		client := newTestClient(t)
		initializeDAP(t, client)
		launchDAP(t, client, program, root, true)

		disconnect := client.request("disconnect")
		client.send(&protocol.DisconnectRequest{Request: disconnect})
		if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success || response.Command != "launch" {
			t.Fatalf("pending launch response = %#v", response)
		}
		if response, ok := client.read().(*protocol.DisconnectResponse); !ok || !response.Success {
			t.Fatalf("disconnect response = %#v", response)
		}

		waitForDAPStop(t, client)
	})

	t.Run("terminate", func(t *testing.T) {
		root := t.TempDir()
		program := writeDAPProgram(t, root, "RETURN 1")
		client := newTestClient(t)
		initializeDAP(t, client)
		launchDAP(t, client, program, root, true)

		terminate := client.request("terminate")
		client.send(&protocol.TerminateRequest{Request: terminate})
		if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success || response.Command != "launch" {
			t.Fatalf("pending launch response = %#v", response)
		}
		if response, ok := client.read().(*protocol.TerminateResponse); !ok || !response.Success {
			t.Fatalf("terminate response = %#v", response)
		}
		if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
			t.Fatal("expected terminated event")
		}

		client.disconnect()
	})
}

func TestDAPDisconnectWhileRunningCleansUp(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN FOR i IN 1..10000000\n  RETURN i")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", response)
	}
	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}
	if entry, ok := client.read().(*protocol.StoppedEvent); !ok || entry.Body.Reason != "entry" {
		t.Fatalf("entry event = %#v", entry)
	}

	continueRequest := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	client.read()
	client.disconnect()
}

func TestDAPStepInStepOutPauseAndTerminate(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, `FUNC add(a) {
  LET b = a + 1
  RETURN b
}
LET x = add(1)
RETURN FOR i IN 1..10000000
  RETURN i + x`)
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", response)
	}
	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}
	if entry, ok := client.read().(*protocol.StoppedEvent); !ok || entry.Body.Reason != "entry" {
		t.Fatalf("entry event = %#v", entry)
	}

	stepIn := client.request("stepIn")
	client.send(&protocol.StepInRequest{
		Request:   stepIn,
		Arguments: protocol.StepInArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.StepInResponse); !ok || !response.Success {
		t.Fatalf("stepIn response = %#v", response)
	}
	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "step" {
		t.Fatalf("stepIn event = %#v", stopped)
	}

	stepOut := client.request("stepOut")
	client.send(&protocol.StepOutRequest{
		Request:   stepOut,
		Arguments: protocol.StepOutArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.StepOutResponse); !ok || !response.Success {
		t.Fatalf("stepOut response = %#v", response)
	}
	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "step" {
		t.Fatalf("stepOut event = %#v", stopped)
	}

	continueRequest := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("continue response = %#v", response)
	}

	pause := client.request("pause")
	client.send(&protocol.PauseRequest{
		Request:   pause,
		Arguments: protocol.PauseArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.PauseResponse); !ok || !response.Success {
		t.Fatalf("pause response = %#v", response)
	}
	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "pause" {
		t.Fatalf("pause event = %#v", stopped)
	}

	terminate := client.request("terminate")
	client.send(&protocol.TerminateRequest{Request: terminate})
	if response, ok := client.read().(*protocol.TerminateResponse); !ok || !response.Success {
		t.Fatalf("terminate response = %#v", response)
	}
	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("expected terminated event")
	}

	client.disconnect()
}

func initializeDAP(t *testing.T, client *testClient) {
	t.Helper()

	initialize := client.request("initialize")
	client.send(&protocol.InitializeRequest{
		Request: initialize,
		Arguments: protocol.InitializeRequestArguments{
			AdapterID:       "ferretd",
			LinesStartAt1:   true,
			ColumnsStartAt1: true,
			PathFormat:      "path",
		},
	})
	if response, ok := client.read().(*protocol.InitializeResponse); !ok || !response.Success {
		t.Fatalf("initialize response = %#v", response)
	}
}

func launchDAP(t *testing.T, client *testClient, program, root string, stopOnEntry bool) {
	t.Helper()

	arguments, err := json.Marshal(launchArguments{Program: program, CWD: root, StopOnEntry: stopOnEntry})
	if err != nil {
		t.Fatal(err)
	}
	launch := client.request("launch")
	client.send(&protocol.LaunchRequest{Request: launch, Arguments: arguments})
	if _, ok := client.read().(*protocol.InitializedEvent); !ok {
		t.Fatal("expected initialized event")
	}
}

func writeDAPProgram(t *testing.T, root, content string) string {
	t.Helper()

	path := filepath.Join(root, "query.fql")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	return path
}

func protocolVariablesContain(variables []protocol.Variable, name, value string) bool {
	for _, variable := range variables {
		if variable.Name == name && variable.Value == value {
			return true
		}
	}

	return false
}
