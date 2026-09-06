package dap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/ferretapi"
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

	server, err := New(input, output, Options{})
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
		want   error
	}{
		{name: "nil input", output: io.Discard, want: errNilInput},
		{name: "nil output", input: strings.NewReader(""), want: errNilOutput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := New(test.input, test.output, Options{})
			if !errors.Is(err, test.want) {
				t.Fatalf("New error = %v, want %v", err, test.want)
			}

			if server != nil {
				t.Fatalf("New server = %v, want nil", server)
			}
		})
	}
}

func TestNewConstructsNativeRuntimeAdapter(t *testing.T) {
	server, err := New(strings.NewReader(""), io.Discard, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, ok := server.runtime.(*ferretapi.Runtime); !ok {
		t.Fatalf("New runtime = %T, want native Ferret adapter", server.runtime)
	}

	if err := server.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()

	return newTestClientWithOptions(t, Options{})
}

func newTestClientWithOptions(t *testing.T, options Options) *testClient {
	t.Helper()

	serverInput, clientInput := io.Pipe()
	clientOutput, serverOutput := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())

	server, err := New(serverInput, serverOutput, options)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

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

func TestDAPLaunchIgnoresUnknownMetadataAndPreservesSupportedArguments(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]any
	}{
		{
			name: "client_metadata",
			metadata: map[string]any{
				"type":        "ferret",
				"request":     "launch",
				"name":        "Debug Ferret",
				"__sessionId": "test-session",
			},
		},
		{
			name: "arbitrary_metadata",
			metadata: map[string]any{
				"clientSpecificThing": map[string]any{"foo": "bar"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			program := writeDAPProgram(t, root, "RETURN @input")
			client := newTestClient(t)
			initializeDAP(t, client)

			arguments := map[string]any{
				"program":     filepath.Base(program),
				"cwd":         root,
				"parameters":  map[string]any{"input": "value"},
				"stopOnEntry": true,
			}
			for key, value := range test.metadata {
				arguments[key] = value
			}

			body, err := json.Marshal(arguments)
			if err != nil {
				t.Fatal(err)
			}

			launch := client.request("launch")
			client.send(&protocol.LaunchRequest{Request: launch, Arguments: body})

			if _, ok := client.read().(*protocol.InitializedEvent); !ok {
				t.Fatal("expected initialized event")
			}

			client.server.stateMu.Lock()
			owned := client.server.owned
			client.server.stateMu.Unlock()

			wantIdentity, err := newSourceIdentity(program, root)
			if err != nil {
				t.Fatalf("newSourceIdentity: %v", err)
			}

			if owned.root != root || owned.program != program ||
				!owned.programIdentity.same(wantIdentity) || !owned.stopOnEntry {
				t.Fatalf("owned Session = %+v, want program %q and stopOnEntry", owned, program)
			}

			opened, err := client.server.workspaces.Get(context.Background(), owned.workspace)
			if err != nil {
				t.Fatalf("Get workspace: %v", err)
			}

			if opened.Root() != root {
				t.Fatalf("workspace root = %q, want %q", opened.Root(), root)
			}

			snapshot, err := client.server.debugs.GetSession(context.Background(), owned.debug)
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}

			if snapshot.Parameters["input"] != "value" {
				t.Fatalf("parameters = %#v, want input value", snapshot.Parameters)
			}

			configurationDone := client.request("configurationDone")
			client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})

			if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
				t.Fatalf("configurationDone response = %#v", response)
			}

			if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
				t.Fatalf("launch response = %#v", response)
			}

			if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "entry" {
				t.Fatalf("stopped event = %#v", stopped)
			}

			client.disconnect()
		})
	}
}

func TestDAPLaunchUnknownMetadataPreservesSupportedArgumentValidation(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		want      string
	}{
		{
			name:      "required_program",
			arguments: `{"progam":"query.fql","clientSpecificThing":{"foo":"bar"}}`,
			want:      "launch program is required",
		},
		{
			name:      "malformed_program",
			arguments: `{"program":{"path":"query.fql"},"clientSpecificThing":true}`,
			want:      "invalid launch arguments:",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t)
			initializeDAP(t, client)

			client.sendRawRequest("launch", test.arguments)

			response, ok := client.read().(*protocol.ErrorResponse)
			if !ok || response.Success || !strings.Contains(response.Message, test.want) {
				t.Fatalf("launch response = %#v, want failure containing %q", response, test.want)
			}

			client.disconnect()
		})
	}
}

func TestDAPSetBreakpointsAcceptsEquivalentSourcePaths(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	relativeRequest := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: relativeRequest,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: filepath.Base(program)},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 1}},
		},
	})

	relativeResponse, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !relativeResponse.Success || len(relativeResponse.Body.Breakpoints) != 1 {
		t.Fatalf("relative setBreakpoints response = %#v", relativeResponse)
	}

	relativeBreakpoint := relativeResponse.Body.Breakpoints[0]
	if !relativeBreakpoint.Verified || relativeBreakpoint.Id == 0 ||
		relativeBreakpoint.Source == nil || relativeBreakpoint.Source.Path != program {
		t.Fatalf("relative breakpoint = %#v", relativeBreakpoint)
	}

	absoluteRequest := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: absoluteRequest,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: program},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 1}},
		},
	})

	absoluteResponse, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !absoluteResponse.Success || len(absoluteResponse.Body.Breakpoints) != 1 {
		t.Fatalf("absolute setBreakpoints response = %#v", absoluteResponse)
	}

	if got := absoluteResponse.Body.Breakpoints[0]; got.Id != relativeBreakpoint.Id ||
		got.Source == nil || got.Source.Path != program {
		t.Fatalf("absolute breakpoint = %#v, want stable ID %d", got, relativeBreakpoint.Id)
	}

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})

	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", response)
	}

	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}

	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "entry" {
		t.Fatalf("stopped event = %#v", stopped)
	}

	client.disconnect()
}

func TestDAPSetBreakpointsReturnsUnverifiedForUnownedSource(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")

	other := filepath.Join(root, "other.fql")
	if err := os.WriteFile(other, []byte("RETURN 2"), 0o600); err != nil {
		t.Fatal(err)
	}

	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	requested := []protocol.SourceBreakpoint{
		{Line: 1},
		{Line: 2, Column: 3},
	}
	setBreakpoints := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: setBreakpoints,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: other},
			Breakpoints: requested,
		},
	})

	response, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !response.Success || len(response.Body.Breakpoints) != 2 {
		t.Fatalf("setBreakpoints response = %#v", response)
	}

	for index, breakpoint := range response.Body.Breakpoints {
		requestedBreakpoint := requested[index]
		if breakpoint.Verified || breakpoint.Id != 0 || breakpoint.Message != "breakpoints are only supported for the launched program" ||
			breakpoint.Source == nil || breakpoint.Source.Path != other ||
			breakpoint.Line != requestedBreakpoint.Line || breakpoint.Column != requestedBreakpoint.Column {
			t.Fatalf("breakpoint %d = %#v", index, breakpoint)
		}
	}

	client.server.breakpointMu.Lock()
	stableCount := len(client.server.stableBreakpoints)
	debuggerCount := len(client.server.debuggerBreakpoints)
	client.server.breakpointMu.Unlock()

	if stableCount != 0 || debuggerCount != 0 {
		t.Fatalf(
			"unowned source created breakpoint state: stable=%d debugger=%d",
			stableCount,
			debuggerCount,
		)
	}

	completePendingDAPLaunch(t, client)
	client.disconnect()
}

func TestDAPSetBreakpointsReturnsUnverifiedForUnavailableSource(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")
	missing := filepath.Join(root, "missing.fql")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	setBreakpoints := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: setBreakpoints,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: filepath.Base(missing)},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 7}},
		},
	})

	response, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !response.Success || len(response.Body.Breakpoints) != 1 {
		t.Fatalf("setBreakpoints response = %#v", response)
	}

	breakpoint := response.Body.Breakpoints[0]
	if breakpoint.Verified || breakpoint.Message != "breakpoint source is unavailable" || breakpoint.Source == nil ||
		breakpoint.Source.Path != missing || breakpoint.Line != 7 {
		t.Fatalf("breakpoint = %#v", breakpoint)
	}

	completePendingDAPLaunch(t, client)
	client.disconnect()
}

func TestDAPSetBreakpointsRejectsMalformedSourceForms(t *testing.T) {
	tests := []struct {
		name       string
		pathFormat string
		source     protocol.Source
		want       string
	}{
		{name: "empty_path", pathFormat: "path", source: protocol.Source{}, want: "source path is required"},
		{
			name:       "source_reference",
			pathFormat: "path",
			source:     protocol.Source{Path: "/query.fql", SourceReference: 1},
			want:       "source references are not supported",
		},
		{
			name:       "remote_uri",
			pathFormat: "uri",
			source:     protocol.Source{Path: "https://example.com/query.fql"},
			want:       "unsupported URI scheme",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			program := writeDAPProgram(t, root, "RETURN 1")
			client := newTestClient(t)

			initialize := client.request("initialize")
			client.send(&protocol.InitializeRequest{
				Request: initialize,
				Arguments: protocol.InitializeRequestArguments{
					AdapterID:       "ferretd",
					PathFormat:      test.pathFormat,
					LinesStartAt1:   true,
					ColumnsStartAt1: true,
				},
			})

			if response, ok := client.read().(*protocol.InitializeResponse); !ok || !response.Success {
				t.Fatalf("initialize response = %#v", response)
			}

			launchDAP(t, client, program, root, true)

			setBreakpoints := client.request("setBreakpoints")
			client.send(&protocol.SetBreakpointsRequest{
				Request: setBreakpoints,
				Arguments: protocol.SetBreakpointsArguments{
					Source:      test.source,
					Breakpoints: []protocol.SourceBreakpoint{{Line: 1}},
				},
			})

			response, ok := client.read().(*protocol.ErrorResponse)
			if !ok || response.Success || !strings.Contains(response.Message, test.want) {
				t.Fatalf("setBreakpoints response = %#v, want failure containing %q", response, test.want)
			}

			completePendingDAPLaunch(t, client)
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

	pagedStackTrace := client.request("stackTrace")
	client.send(&protocol.StackTraceRequest{
		Request: pagedStackTrace,
		Arguments: protocol.StackTraceArguments{
			ThreadId:   threadID,
			StartFrame: 1,
			Levels:     2,
		},
	})

	pagedStackResponse, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || !pagedStackResponse.Success || len(pagedStackResponse.Body.StackFrames) != 2 ||
		pagedStackResponse.Body.TotalFrames != 3 {
		t.Fatalf("paged stackTrace response = %#v", pagedStackResponse)
	}

	for offset, frame := range pagedStackResponse.Body.StackFrames {
		wantIndex := 1 + offset
		wantFrame := stackResponse.Body.StackFrames[wantIndex]

		wantFrame.Id = frame.Id
		if !reflect.DeepEqual(frame, wantFrame) {
			t.Fatalf("paged frame = %+v, want %+v", frame, wantFrame)
		}

		if frameIndex, status := client.server.handles.FrameIndex(frame.Id); status != handleCurrent || frameIndex != wantIndex {
			t.Fatalf("paged frame handle = (%d, %v), want (%d, current)", frameIndex, status, wantIndex)
		}
	}

	finalStackTrace := client.request("stackTrace")
	client.send(&protocol.StackTraceRequest{
		Request: finalStackTrace,
		Arguments: protocol.StackTraceArguments{
			ThreadId:   threadID,
			StartFrame: 2,
			Levels:     1,
		},
	})

	finalStackResponse, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || !finalStackResponse.Success || len(finalStackResponse.Body.StackFrames) != 1 ||
		finalStackResponse.Body.TotalFrames != 3 {
		t.Fatalf("final stackTrace page response = %#v", finalStackResponse)
	}

	finalFrame := finalStackResponse.Body.StackFrames[0]
	wantFinalFrame := stackResponse.Body.StackFrames[2]

	wantFinalFrame.Id = finalFrame.Id
	if !reflect.DeepEqual(finalFrame, wantFinalFrame) {
		t.Fatalf("final page frame = %+v, want %+v", finalFrame, wantFinalFrame)
	}

	if frameIndex, status := client.server.handles.FrameIndex(finalFrame.Id); status != handleCurrent || frameIndex != 2 {
		t.Fatalf("final page frame handle = (%d, %v), want (2, current)", frameIndex, status)
	}

	scopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   scopes,
		Arguments: protocol.ScopesArguments{FrameId: pagedStackResponse.Body.StackFrames[0].Id},
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
			Expression: "p + @input",
			FrameId:    pagedStackResponse.Body.StackFrames[0].Id,
			Context:    "hover",
		},
	})

	evaluateResponse, ok := client.read().(*protocol.EvaluateResponse)
	if !ok || evaluateResponse.Body.Result != "4" || evaluateResponse.Body.Type != "Float" {
		t.Fatalf("evaluate response = %#v", evaluateResponse)
	}

	for _, mainFrame := range []protocol.StackFrame{pagedStackResponse.Body.StackFrames[1], finalFrame} {
		mainScopes := client.request("scopes")
		client.send(&protocol.ScopesRequest{
			Request:   mainScopes,
			Arguments: protocol.ScopesArguments{FrameId: mainFrame.Id},
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

		mainEvaluate := client.request("evaluate")
		client.send(&protocol.EvaluateRequest{
			Request: mainEvaluate,
			Arguments: protocol.EvaluateArguments{
				Expression: "box.value",
				FrameId:    mainFrame.Id,
				Context:    "hover",
			},
		})

		mainEvaluateResponse, ok := client.read().(*protocol.EvaluateResponse)
		if !ok || !mainEvaluateResponse.Success || mainEvaluateResponse.Body.Result != "10" ||
			mainEvaluateResponse.Body.Type != "Int" {
			t.Fatalf("main evaluate response = %#v", mainEvaluateResponse)
		}
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
		Arguments: protocol.ScopesArguments{FrameId: pagedStackResponse.Body.StackFrames[0].Id},
	})

	if response, ok := client.read().(*protocol.ScopesResponse); !ok || !response.Success ||
		len(response.Body.Scopes) != 0 {
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

func TestDAPLateInspectionHandlesStayStaleAcrossRecursiveStops(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "FUNC fib(n) {\n"+
		"  RETURN MATCH n {\n"+
		"    0 => 0,\n"+
		"    1 => 1,\n"+
		"    _ => fib(n - 1) + fib(n - 2),\n"+
		"  }\n"+
		"}\n"+
		"RETURN fib(6)")
	var diagnostics bytes.Buffer
	logger := newCaptureLogger(&diagnostics, zerolog.DebugLevel)
	client := newTestClientWithOptions(t, Options{Logger: logger})
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)
	completePendingDAPLaunch(t, client)

	stackTrace := client.request("stackTrace")
	client.send(&protocol.StackTraceRequest{
		Request:   stackTrace,
		Arguments: protocol.StackTraceArguments{ThreadId: threadID},
	})

	stackResponse, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || len(stackResponse.Body.StackFrames) != 1 {
		t.Fatalf("entry stackTrace response = %#v", stackResponse)
	}

	oldFrameID := stackResponse.Body.StackFrames[0].Id

	scopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   scopes,
		Arguments: protocol.ScopesArguments{FrameId: oldFrameID},
	})

	scopesResponse, ok := client.read().(*protocol.ScopesResponse)
	if !ok || len(scopesResponse.Body.Scopes) != 2 {
		t.Fatalf("entry scopes response = %#v", scopesResponse)
	}

	oldScopeID := scopesResponse.Body.Scopes[0].VariablesReference

	stepIn := client.request("stepIn")
	client.send(&protocol.StepInRequest{
		Request:   stepIn,
		Arguments: protocol.StepInArguments{ThreadId: threadID},
	})

	if response, ok := client.read().(*protocol.StepInResponse); !ok || !response.Success {
		t.Fatalf("stepIn response = %#v", response)
	}

	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "step" {
		t.Fatalf("stepIn stopped event = %#v", stopped)
	}

	lateScopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   lateScopes,
		Arguments: protocol.ScopesArguments{FrameId: oldFrameID},
	})

	if response, ok := client.read().(*protocol.ScopesResponse); !ok || !response.Success ||
		len(response.Body.Scopes) != 0 {
		t.Fatalf("late scopes response = %#v", response)
	}

	lateVariables := client.request("variables")
	client.send(&protocol.VariablesRequest{
		Request: lateVariables,
		Arguments: protocol.VariablesArguments{
			VariablesReference: oldScopeID,
		},
	})

	if response, ok := client.read().(*protocol.VariablesResponse); !ok || !response.Success ||
		len(response.Body.Variables) != 0 {
		t.Fatalf("late variables response = %#v", response)
	}

	for _, contextName := range []string{"hover", "watch"} {
		evaluate := client.request("evaluate")
		client.send(&protocol.EvaluateRequest{
			Request: evaluate,
			Arguments: protocol.EvaluateArguments{
				Expression: "n",
				FrameId:    oldFrameID,
				Context:    contextName,
			},
		})

		response, ok := client.read().(*protocol.EvaluateResponse)
		if !ok || !response.Success || response.Body.Result != "" ||
			response.Body.VariablesReference != 0 {
			t.Fatalf("stale %s evaluate response = %#v", contextName, response)
		}
	}

	for _, test := range []struct {
		name    string
		context string
		frameID int
	}{
		{name: "repl", context: "repl", frameID: oldFrameID},
		{name: "omitted context", frameID: oldFrameID},
		{name: "unfamiliar context", context: "future-client", frameID: oldFrameID},
		{name: "unknown hover frame", context: "hover", frameID: oldFrameID + 1_000_000},
	} {
		evaluate := client.request("evaluate")
		client.send(&protocol.EvaluateRequest{
			Request: evaluate,
			Arguments: protocol.EvaluateArguments{
				Expression: "n",
				FrameId:    test.frameID,
				Context:    test.context,
			},
		})
		assertDAPFailure(t, client.read(), "evaluate", "stack frame handle is stale or invalid")
	}

	newStackTrace := client.request("stackTrace")
	client.send(&protocol.StackTraceRequest{
		Request:   newStackTrace,
		Arguments: protocol.StackTraceArguments{ThreadId: threadID},
	})

	newStackResponse, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || len(newStackResponse.Body.StackFrames) < 2 {
		t.Fatalf("new stackTrace response = %#v", newStackResponse)
	}

	newFrameIDs := make(map[int]struct{}, len(newStackResponse.Body.StackFrames))
	for _, frame := range newStackResponse.Body.StackFrames {
		if frame.Id == oldFrameID {
			t.Fatalf("old frame handle %d reused by new stack: %#v", oldFrameID, newStackResponse.Body.StackFrames)
		}

		if _, exists := newFrameIDs[frame.Id]; exists {
			t.Fatalf("recursive stack frame handle %d is duplicated: %#v", frame.Id, newStackResponse.Body.StackFrames)
		}

		newFrameIDs[frame.Id] = struct{}{}
	}

	lateScopesAfterStack := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   lateScopesAfterStack,
		Arguments: protocol.ScopesArguments{FrameId: oldFrameID},
	})

	if response, ok := client.read().(*protocol.ScopesResponse); !ok || !response.Success ||
		len(response.Body.Scopes) != 0 {
		t.Fatalf("late scopes response after new stack = %#v", response)
	}

	newScopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   newScopes,
		Arguments: protocol.ScopesArguments{FrameId: newStackResponse.Body.StackFrames[0].Id},
	})

	if response, ok := client.read().(*protocol.ScopesResponse); !ok || !response.Success ||
		len(response.Body.Scopes) != 2 {
		t.Fatalf("new scopes response = %#v", response)
	}

	randomScopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   randomScopes,
		Arguments: protocol.ScopesArguments{FrameId: oldFrameID + 1_000_000},
	})
	assertDAPFailure(t, client.read(), "scopes", "stack frame handle is stale or invalid")

	randomVariables := client.request("variables")
	client.send(&protocol.VariablesRequest{
		Request: randomVariables,
		Arguments: protocol.VariablesArguments{
			VariablesReference: oldScopeID + 1_000_000,
		},
	})
	assertDAPFailure(t, client.read(), "variables", "variable handle is stale or invalid")

	malformedScopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   malformedScopes,
		Arguments: protocol.ScopesArguments{FrameId: -1},
	})
	assertDAPFailure(t, client.read(), "scopes", "stack frame handle is stale or invalid")

	client.disconnect()

	records := decodeDiagnostics(t, diagnostics.Bytes())
	requireDiagnostic(t, records, diagnosticRecord{
		"level":       "debug",
		"message":     "DAP stack frame handle allocated",
		"frame_id":    oldFrameID,
		"frame_index": 0,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level":         "debug",
		"message":       "DAP handles invalidated",
		"cause":         "stepIn",
		"frames":        1,
		"scopes":        2,
		"variables":     0,
		"stale_handles": 3,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level":     "debug",
		"message":   "DAP response",
		"command":   "scopes",
		"frame_id":  oldFrameID,
		"scopes":    0,
		"stale":     true,
		"success":   true,
		"direction": "->",
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level":               "debug",
		"message":             "DAP response",
		"command":             "variables",
		"variables_reference": oldScopeID,
		"variables":           0,
		"stale":               true,
		"success":             true,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level":             "debug",
		"message":           "DAP response",
		"command":           "evaluate",
		"context":           "hover",
		"frame_id":          oldFrameID,
		"expression_length": 1,
		"stale":             true,
		"success":           true,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level":       "warn",
		"message":     "DAP request failed",
		"command":     "scopes",
		"request_seq": randomScopes.Seq,
		"frame_id":    oldFrameID + 1_000_000,
	})
	for _, record := range records {
		if record["message"] == "DAP request failed" &&
			record["request_seq"] == float64(lateScopes.Seq) {
			t.Fatalf("recognized stale scopes request logged as a failure: %#v", record)
		}
	}
}

func TestDAPEvaluateEmptyExpressionsReturnEmptyResult(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "LET value = 1\nRETURN value")
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)
	completePendingDAPLaunch(t, client)

	stackTrace := client.request("stackTrace")
	client.send(&protocol.StackTraceRequest{
		Request:   stackTrace,
		Arguments: protocol.StackTraceArguments{ThreadId: threadID},
	})

	stackResponse, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || len(stackResponse.Body.StackFrames) == 0 {
		t.Fatalf("stackTrace response = %#v", stackResponse)
	}

	frameID := stackResponse.Body.StackFrames[0].Id

	tests := []struct {
		name       string
		expression string
		context    string
		frameID    int
	}{
		{name: "hover", context: "hover", frameID: frameID},
		{name: "watch", context: "watch", frameID: frameID},
		{name: "repl", context: "repl", frameID: frameID},
		{name: "whitespace", expression: " \t\n", context: "watch", frameID: frameID},
		{name: "no frame", context: "hover"},
		{name: "stale frame", context: "watch", frameID: frameID + 1000},
		{name: "omitted context", frameID: frameID},
		{name: "unfamiliar context", context: "future-client", frameID: frameID},
	}

	for _, test := range tests {
		evaluate := client.request("evaluate")
		client.send(&protocol.EvaluateRequest{
			Request: evaluate,
			Arguments: protocol.EvaluateArguments{
				Expression: test.expression,
				FrameId:    test.frameID,
				Context:    test.context,
			},
		})

		response, ok := client.read().(*protocol.EvaluateResponse)
		if !ok || !response.Success || response.Command != "evaluate" ||
			response.Body.Result != "" || response.Body.Type != "" ||
			response.Body.VariablesReference != 0 || response.Body.PresentationHint != nil ||
			response.Body.NamedVariables != 0 || response.Body.IndexedVariables != 0 ||
			response.Body.MemoryReference != "" {
			t.Fatalf("%s evaluate response = %#v", test.name, response)
		}
	}

	evaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: evaluate,
		Arguments: protocol.EvaluateArguments{
			Expression: "1 + 1",
			FrameId:    frameID,
			Context:    "repl",
		},
	})

	response, ok := client.read().(*protocol.EvaluateResponse)
	if !ok || !response.Success || response.Body.Result != "2" {
		t.Fatalf("non-empty evaluate response = %#v", response)
	}

	failedEvaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: failedEvaluate,
		Arguments: protocol.EvaluateArguments{
			Expression: "missing_value",
			FrameId:    frameID,
			Context:    "watch",
		},
	})

	failure, ok := client.read().(*protocol.ErrorResponse)
	if !ok || failure.Success || failure.Command != "evaluate" || failure.Message == "" ||
		failure.Body.Error == nil || !failure.Body.Error.ShowUser {
		t.Fatalf("failed non-empty evaluate response = %#v", failure)
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

func completePendingDAPLaunch(t *testing.T, client *testClient) {
	t.Helper()

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})

	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", response)
	}

	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}

	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "entry" {
		t.Fatalf("stopped event = %#v", stopped)
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
