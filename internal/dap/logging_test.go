package dap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"
)

func TestDAPInfoDiagnosticsLogFailuresWithoutDebugTraffic(t *testing.T) {
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 1")
	other := filepath.Join(root, "other.fql")
	if err := os.WriteFile(other, []byte("RETURN 2"), 0o600); err != nil {
		t.Fatalf("write other program: %v", err)
	}
	programCanonical, err := filepath.EvalSymlinks(program)
	if err != nil {
		t.Fatalf("canonicalize program: %v", err)
	}
	otherCanonical, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatalf("canonicalize other program: %v", err)
	}

	var diagnostics bytes.Buffer
	logger := newCaptureLogger(&diagnostics, zerolog.InfoLevel)
	client := newTestClientWithOptions(t, Options{Logger: logger})
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)

	setBreakpoints := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: setBreakpoints,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: other},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 1}},
		},
	})
	response, ok := client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !response.Success || len(response.Body.Breakpoints) != 1 || response.Body.Breakpoints[0].Verified {
		t.Fatalf("unowned setBreakpoints response = %#v", response)
	}

	missing := filepath.Join(root, "missing.fql")
	setMissingBreakpoints := client.request("setBreakpoints")
	client.send(&protocol.SetBreakpointsRequest{
		Request: setMissingBreakpoints,
		Arguments: protocol.SetBreakpointsArguments{
			Source:      protocol.Source{Path: missing},
			Breakpoints: []protocol.SourceBreakpoint{{Line: 1}},
		},
	})
	response, ok = client.read().(*protocol.SetBreakpointsResponse)
	if !ok || !response.Success || len(response.Body.Breakpoints) != 1 || response.Body.Breakpoints[0].Verified {
		t.Fatalf("unavailable setBreakpoints response = %#v", response)
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

	scopes := client.request("scopes")
	client.send(&protocol.ScopesRequest{
		Request:   scopes,
		Arguments: protocol.ScopesArguments{FrameId: 37},
	})
	assertDAPFailure(t, client.read(), "scopes", "stack frame handle is stale or invalid")

	evaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: evaluate,
		Arguments: protocol.EvaluateArguments{
			Context: "repl",
			FrameId: 37,
		},
	})
	if response, ok := client.read().(*protocol.EvaluateResponse); !ok || !response.Success ||
		response.Body.Result != "" || response.Body.VariablesReference != 0 {
		t.Fatalf("empty evaluate response = %#v", response)
	}
	client.disconnect()

	records := decodeDiagnostics(t, diagnostics.Bytes())
	requireDiagnostic(t, records, diagnosticRecord{
		"level": "info", "component": "dap", "message": "DAP session started",
	})
	created := requireDiagnostic(t, records, diagnosticRecord{
		"level":         "info",
		"message":       "DAP debug session created",
		"program":       program,
		"root":          root,
		"stop_on_entry": true,
	})
	for _, key := range []string{"workspace_id", "execution_session_id", "debug_session_id"} {
		if value, ok := created[key].(string); !ok || value == "" {
			t.Fatalf("created diagnostic %s = %#v", key, created[key])
		}
	}
	requireDiagnostic(t, records, diagnosticRecord{
		"level": "info", "message": "DAP configuration completed",
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level": "info", "message": "DAP execution stopped", "reason": "entry", "thread_id": 1,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level":                    "warn",
		"message":                  "DAP breakpoint source does not match launched program",
		"source":                   other,
		"source_canonical":         otherCanonical,
		"launch_program":           program,
		"launch_program_canonical": programCanonical,
		"count":                    1,
	})
	unavailable := requireDiagnostic(t, records, diagnosticRecord{
		"level":                    "warn",
		"message":                  "DAP breakpoint source is unavailable",
		"source":                   missing,
		"launch_program":           program,
		"launch_program_canonical": programCanonical,
		"count":                    1,
	})
	if value, ok := unavailable["error"].(string); !ok || value == "" {
		t.Fatalf("unavailable diagnostic error = %#v", unavailable["error"])
	}
	requireDiagnostic(t, records, diagnosticRecord{
		"level": "warn", "message": "DAP request failed", "command": "scopes", "frame_id": 37,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"level": "info", "message": "DAP session ended", "status": "completed",
	})

	for _, record := range records {
		if record["command"] == "evaluate" {
			t.Fatalf("info diagnostics contain empty evaluate traffic: %#v", record)
		}

		if record["message"] == "DAP request" || record["message"] == "DAP response" || record["message"] == "DAP event" {
			t.Fatalf("info diagnostics contain debug traffic: %#v", record)
		}
	}
}

func TestDAPDebugTraceSelectsMetadataWithoutSensitiveValues(t *testing.T) {
	const (
		sourceSecret     = "SOURCE_CONTENT_MUST_NOT_BE_LOGGED"
		parameterKey     = "PARAMETER_KEY_MUST_NOT_BE_LOGGED"
		parameterSecret  = "PARAMETER_VALUE_MUST_NOT_BE_LOGGED"
		evaluationSecret = "EVALUATION_VALUE_MUST_NOT_BE_LOGGED"
		failureSecret    = "FAILURE_EXPRESSION_MUST_NOT_BE_LOGGED"
	)

	root := t.TempDir()
	program := writeDAPProgram(t, root, `LET secret = "`+sourceSecret+`"
RETURN @`+parameterKey)
	var diagnostics bytes.Buffer
	logger := newCaptureLogger(&diagnostics, zerolog.DebugLevel)
	client := newTestClientWithOptions(t, Options{Logger: logger})
	initializeDAP(t, client)

	arguments, err := json.Marshal(launchArguments{
		Program:     program,
		CWD:         root,
		Parameters:  map[string]any{parameterKey: parameterSecret},
		StopOnEntry: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	launch := client.request("launch")
	client.send(&protocol.LaunchRequest{Request: launch, Arguments: arguments})
	if _, ok := client.read().(*protocol.InitializedEvent); !ok {
		t.Fatal("expected initialized event")
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

	expression := `"` + evaluationSecret + `"`
	evaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: evaluate,
		Arguments: protocol.EvaluateArguments{
			Expression: expression,
			Context:    "watch",
		},
	})
	response, ok := client.read().(*protocol.EvaluateResponse)
	if !ok || !response.Success {
		t.Fatalf("evaluate response = %#v", response)
	}

	failedEvaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: failedEvaluate,
		Arguments: protocol.EvaluateArguments{
			Expression: failureSecret,
			Context:    "watch",
		},
	})
	failure, ok := client.read().(*protocol.ErrorResponse)
	if !ok || failure.Success || !strings.Contains(failure.Message, failureSecret) {
		t.Fatalf("failed evaluate response = %#v", failure)
	}

	emptyEvaluate := client.request("evaluate")
	client.send(&protocol.EvaluateRequest{
		Request: emptyEvaluate,
		Arguments: protocol.EvaluateArguments{
			Context: "repl",
			FrameId: 37,
		},
	})
	emptyResponse, ok := client.read().(*protocol.EvaluateResponse)
	if !ok || !emptyResponse.Success || emptyResponse.Body.Result != "" ||
		emptyResponse.Body.VariablesReference != 0 {
		t.Fatalf("empty evaluate response = %#v", emptyResponse)
	}

	continueRequest := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("continue response = %#v", response)
	}
	if output, ok := client.read().(*protocol.OutputEvent); !ok ||
		output.Body.Category != "stdout" || !strings.Contains(output.Body.Output, parameterSecret) {
		t.Fatalf("output event = %#v", output)
	}
	if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 0 {
		t.Fatalf("exited event = %#v", exited)
	}
	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("expected terminated event")
	}
	client.disconnect()

	records := decodeDiagnostics(t, diagnostics.Bytes())
	wantTrace := []string{
		"request:initialize:1",
		"response:initialize:1:1",
		"request:launch:2",
		"event:initialized:2",
		"request:configurationDone:3",
		"response:configurationDone:3:3",
		"response:launch:2:4",
		"event:stopped:5",
		"request:evaluate:4",
		"response:evaluate:4:6",
		"request:evaluate:5",
		"failure:evaluate:5",
		"request:evaluate:6",
		"response:evaluate:6:8",
		"request:continue:7",
		"response:continue:7:9",
		"event:output:10",
		"event:exited:11",
		"event:terminated:12",
		"request:disconnect:8",
		"response:disconnect:8:13",
	}
	gotTrace := traceSignatures(records)
	// A client can send its next request after reading an event but before the
	// event's post-write trace runs. Sequence fields retain the logical order.
	slices.Sort(gotTrace)
	slices.Sort(wantTrace)
	if !reflect.DeepEqual(gotTrace, wantTrace) {
		t.Fatalf("trace signatures = %#v, want %#v", gotTrace, wantTrace)
	}

	requireDiagnostic(t, records, diagnosticRecord{
		"message": "DAP request", "command": "initialize", "direction": "<-", "kind": "request", "request_seq": 1,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"message": "DAP response", "command": "initialize", "direction": "->", "kind": "response", "success": true,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"message": "DAP event", "event": "initialized", "direction": "->", "kind": "event",
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"message": "DAP request", "command": "evaluate", "context": "watch", "frame_id": 0,
		"expression_length": len(expression),
	})
	failureRecord := requireDiagnostic(t, records, diagnosticRecord{
		"message": "DAP request failed", "command": "evaluate", "error": "debug evaluation failed",
	})
	if errorType, ok := failureRecord["error_type"].(string); !ok || errorType == "" {
		t.Fatalf("evaluation failure error_type = %#v", failureRecord["error_type"])
	}
	requireDiagnostic(t, records, diagnosticRecord{
		"message":           "DAP request",
		"command":           "evaluate",
		"request_seq":       6,
		"context":           "repl",
		"frame_id":          37,
		"expression_length": 0,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"message":           "DAP response",
		"command":           "evaluate",
		"request_seq":       6,
		"success":           true,
		"context":           "repl",
		"frame_id":          37,
		"expression_length": 0,
		"empty":             true,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		"message": "DAP event", "event": "output", "category": "stdout",
	})

	for _, secret := range []string{
		sourceSecret,
		parameterKey,
		parameterSecret,
		evaluationSecret,
		expression,
		failureSecret,
	} {
		if bytes.Contains(diagnostics.Bytes(), []byte(secret)) {
			t.Fatalf("diagnostics contain sensitive value %q: %q", secret, diagnostics.String())
		}
	}
}

func TestDAPDebugLoggingPreservesProtocolOutput(t *testing.T) {
	input := func() []byte {
		var stream bytes.Buffer
		initialize := protocol.Request{
			ProtocolMessage: protocol.ProtocolMessage{Seq: 1, Type: "request"},
			Command:         "initialize",
		}
		if err := protocol.WriteProtocolMessage(&stream, &protocol.InitializeRequest{
			Request: initialize,
			Arguments: protocol.InitializeRequestArguments{
				AdapterID:       "ferretd",
				LinesStartAt1:   true,
				ColumnsStartAt1: true,
				PathFormat:      "path",
			},
		}); err != nil {
			t.Fatalf("write initialize: %v", err)
		}

		disconnect := protocol.Request{
			ProtocolMessage: protocol.ProtocolMessage{Seq: 2, Type: "request"},
			Command:         "disconnect",
		}
		if err := protocol.WriteProtocolMessage(&stream, &protocol.DisconnectRequest{Request: disconnect}); err != nil {
			t.Fatalf("write disconnect: %v", err)
		}

		return stream.Bytes()
	}()

	run := func(options Options) []byte {
		t.Helper()

		var output bytes.Buffer
		server, err := New(bytes.NewReader(input), &output, options)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := server.Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}

		return output.Bytes()
	}

	want := run(Options{})
	var diagnostics bytes.Buffer
	logger := newCaptureLogger(&diagnostics, zerolog.DebugLevel)
	got := run(Options{Logger: logger})
	if !bytes.Equal(got, want) {
		t.Fatalf("debug protocol output differs:\n got %q\nwant %q", got, want)
	}
	if bytes.Contains(got, []byte("DAP request")) || bytes.Contains(got, []byte("component=dap")) {
		t.Fatalf("protocol output contains diagnostics: %q", got)
	}

	reader := bufio.NewReader(bytes.NewReader(got))
	if message, err := protocol.ReadProtocolMessage(reader); err != nil {
		t.Fatalf("read initialize response: %v", err)
	} else if response, ok := message.(*protocol.InitializeResponse); !ok || !response.Success {
		t.Fatalf("initialize response = %#v", message)
	}
	if message, err := protocol.ReadProtocolMessage(reader); err != nil {
		t.Fatalf("read disconnect response: %v", err)
	} else if response, ok := message.(*protocol.DisconnectResponse); !ok || !response.Success {
		t.Fatalf("disconnect response = %#v", message)
	}
	if _, err := protocol.ReadProtocolMessage(reader); err != io.EOF {
		t.Fatalf("protocol output trailing read error = %v, want EOF", err)
	}
}

type diagnosticRecord map[string]any

func newCaptureLogger(output io.Writer, level zerolog.Level) *zerolog.Logger {
	logger := zerolog.New(zerolog.SyncWriter(output)).Level(level)

	return &logger
}

func decodeDiagnostics(t *testing.T, data []byte) []diagnosticRecord {
	t.Helper()

	scanner := bufio.NewScanner(bytes.NewReader(data))
	records := make([]diagnosticRecord, 0)
	for scanner.Scan() {
		var record diagnosticRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode diagnostic %q: %v", scanner.Text(), err)
		}

		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan diagnostics: %v", err)
	}

	return records
}

func requireDiagnostic(t *testing.T, records []diagnosticRecord, fields diagnosticRecord) diagnosticRecord {
	t.Helper()

	for _, record := range records {
		if diagnosticMatches(record, fields) {
			return record
		}
	}

	t.Fatalf("diagnostics = %#v, want record containing %#v", records, fields)

	return nil
}

func diagnosticMatches(record, fields diagnosticRecord) bool {
	for key, want := range fields {
		got, ok := record[key]
		if !ok {
			return false
		}

		if wantInt, ok := want.(int); ok {
			gotNumber, ok := got.(float64)
			if !ok || gotNumber != float64(wantInt) {
				return false
			}

			continue
		}

		if !reflect.DeepEqual(got, want) {
			return false
		}
	}

	return true
}

func traceSignatures(records []diagnosticRecord) []string {
	result := make([]string, 0)
	for _, record := range records {
		switch record["message"] {
		case "DAP request":
			result = append(result, fmt.Sprintf(
				"request:%v:%v",
				record["command"],
				record["request_seq"],
			))
		case "DAP response":
			result = append(result, fmt.Sprintf(
				"response:%v:%v:%v",
				record["command"],
				record["request_seq"],
				record["response_seq"],
			))
		case "DAP event":
			result = append(result, fmt.Sprintf(
				"event:%v:%v",
				record["event"],
				record["event_seq"],
			))
		case "DAP request failed":
			result = append(result, fmt.Sprintf(
				"failure:%v:%v",
				record["command"],
				record["request_seq"],
			))
		}
	}

	return result
}

func assertDAPFailure(t *testing.T, message protocol.Message, command, want string) {
	t.Helper()

	response, ok := message.(*protocol.ErrorResponse)
	if !ok || response.Success || response.Command != command || response.Message != want ||
		response.Body.Error == nil || response.Body.Error.Format != want {
		t.Fatalf("%s response = %#v, want failure %q", command, message, want)
	}
}
