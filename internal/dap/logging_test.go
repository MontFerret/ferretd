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
	assertDAPFailure(t, client.read(), "setBreakpoints", "breakpoint source must match the launched program")

	configurationDone := client.request("configurationDone")
	client.send(&protocol.ConfigurationDoneRequest{Request: configurationDone})
	if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success {
		t.Fatalf("configurationDone response = %#v", response)
	}
	if response, ok := client.read().(*protocol.LaunchResponse); !ok || !response.Success {
		t.Fatalf("launch response = %#v", response)
	}
	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != stopReasonEntry.String() {
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
			Context: "watch",
		},
	})
	assertDAPFailure(t, client.read(), "evaluate", "invalid argument: debug expression is empty")
	client.disconnect()

	records := decodeDiagnostics(t, diagnostics.Bytes())
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName: "info", logFieldComponent.FieldName(): logComponentDAP.String(),
		zerolog.MessageFieldName: logMessageSessionStarted,
	})
	created := requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName:          "info",
		zerolog.MessageFieldName:        logMessageDebugSessionCreated,
		logFieldProgram.FieldName():     program,
		logFieldRoot.FieldName():        root,
		logFieldStopOnEntry.FieldName(): true,
	})
	for _, field := range []logField{logFieldWorkspaceID, logFieldExecutionSessionID, logFieldDebugSessionID} {
		value := created.field(field)
		if stringValue, ok := value.(string); !ok || stringValue == "" {
			t.Fatalf("created diagnostic %s = %#v", field, value)
		}
	}
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName: "info", zerolog.MessageFieldName: logMessageConfigurationCompleted,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName: "info", zerolog.MessageFieldName: logMessageExecutionStopped,
		logFieldReason.FieldName(): stopReasonEntry.String(), logFieldThreadID.FieldName(): 1,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName:            "warn",
		zerolog.MessageFieldName:          logMessageRequestFailed,
		logFieldCommand.FieldName():       "setBreakpoints",
		logFieldLaunchProgram.FieldName(): program,
		zerolog.ErrorFieldName:            "breakpoint source must match the launched program",
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName: "warn", zerolog.MessageFieldName: logMessageRequestFailed,
		logFieldCommand.FieldName(): "scopes", logFieldFrameID.FieldName(): 37,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName:               "warn",
		zerolog.MessageFieldName:             logMessageRequestFailed,
		logFieldCommand.FieldName():          "evaluate",
		logFieldContext.FieldName():          "watch",
		logFieldExpressionLength.FieldName(): 0,
		zerolog.ErrorFieldName:               "invalid argument: debug expression is empty",
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.LevelFieldName: "info", zerolog.MessageFieldName: logMessageSessionEnded,
		logFieldStatus.FieldName(): sessionEndCompleted.String(),
	})

	for _, record := range records {
		message := record[zerolog.MessageFieldName]
		if message == logMessageRequest || message == logMessageResponse || message == logMessageEvent {
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
	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != stopReasonEntry.String() {
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

	continueRequest := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request:   continueRequest,
		Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})
	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("continue response = %#v", response)
	}
	if output, ok := client.read().(*protocol.OutputEvent); !ok ||
		output.Body.Category != outputCategoryStdout.String() || !strings.Contains(output.Body.Output, parameterSecret) {
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
		"request:continue:6",
		"response:continue:6:8",
		"event:output:9",
		"event:exited:10",
		"event:terminated:11",
		"request:disconnect:7",
		"response:disconnect:7:12",
	}
	if got := traceSignatures(records); !reflect.DeepEqual(got, wantTrace) {
		t.Fatalf("trace signatures = %#v, want %#v", got, wantTrace)
	}

	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.MessageFieldName: logMessageRequest, logFieldCommand.FieldName(): "initialize",
		logFieldDirection.FieldName(): logDirectionInbound.String(), logFieldKind.FieldName(): logKindRequest.String(),
		logFieldRequestSequence.FieldName(): 1,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.MessageFieldName: logMessageResponse, logFieldCommand.FieldName(): "initialize",
		logFieldDirection.FieldName(): logDirectionOutbound.String(), logFieldKind.FieldName(): logKindResponse.String(),
		logFieldSuccess.FieldName(): true,
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.MessageFieldName: logMessageEvent, logFieldEvent.FieldName(): eventInitialized,
		logFieldDirection.FieldName(): logDirectionOutbound.String(), logFieldKind.FieldName(): logKindEvent.String(),
	})
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.MessageFieldName: logMessageRequest, logFieldCommand.FieldName(): "evaluate",
		logFieldContext.FieldName(): "watch", logFieldFrameID.FieldName(): 0,
		logFieldExpressionLength.FieldName(): len(expression),
	})
	failureRecord := requireDiagnostic(t, records, diagnosticRecord{
		zerolog.MessageFieldName: logMessageRequestFailed, logFieldCommand.FieldName(): "evaluate",
		zerolog.ErrorFieldName: "debug evaluation failed",
	})
	if errorType, ok := failureRecord.field(logFieldErrorType).(string); !ok || errorType == "" {
		t.Fatalf("evaluation failure error_type = %#v", failureRecord.field(logFieldErrorType))
	}
	requireDiagnostic(t, records, diagnosticRecord{
		zerolog.MessageFieldName: logMessageEvent, logFieldEvent.FieldName(): eventOutput,
		logFieldCategory.FieldName(): outputCategoryStdout.String(),
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
	componentDiagnostic := logFieldComponent.FieldName() + "=" + logComponentDAP.String()
	if bytes.Contains(got, []byte(logMessageRequest)) || bytes.Contains(got, []byte(componentDiagnostic)) {
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

func (r diagnosticRecord) field(field logField) any {
	return r[field.FieldName()]
}

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
		switch record[zerolog.MessageFieldName] {
		case logMessageRequest:
			result = append(result, fmt.Sprintf(
				"request:%v:%v",
				record.field(logFieldCommand),
				record.field(logFieldRequestSequence),
			))
		case logMessageResponse:
			result = append(result, fmt.Sprintf(
				"response:%v:%v:%v",
				record.field(logFieldCommand),
				record.field(logFieldRequestSequence),
				record.field(logFieldResponseSequence),
			))
		case logMessageEvent:
			result = append(result, fmt.Sprintf(
				"event:%v:%v",
				record.field(logFieldEvent),
				record.field(logFieldEventSequence),
			))
		case logMessageRequestFailed:
			result = append(result, fmt.Sprintf(
				"failure:%v:%v",
				record.field(logFieldCommand),
				record.field(logFieldRequestSequence),
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
