package dap

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/google/go-dap"
	"github.com/rs/zerolog"

	"github.com/MontFerret/ferretd/internal/exec"
)

func TestDAPLaunchWorkingDirectoryFilesystemAndSourceIdentity(t *testing.T) {
	tests := []struct {
		name     string
		override bool
		outside  bool
		symlink  bool
	}{
		{name: "omitted uses workspace"},
		{name: "inside workspace", override: true},
		{name: "outside workspace", override: true, outside: true},
		{name: "symlink outside workspace", override: true, outside: true, symlink: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()

			queries := filepath.Join(root, "queries")
			if err := os.Mkdir(queries, 0o700); err != nil {
				t.Fatal(err)
			}

			program := writeDAPProgram(t, queries, "LET value = TO_STRING(IO::FS::READ(@file))\nRETURN value")
			runtimeRoot := root

			if test.override {
				parent := root

				if test.outside {
					parent = t.TempDir()
				}

				runtimeRoot = filepath.Join(parent, "runtime root ü")
				if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			if err := os.WriteFile(filepath.Join(runtimeRoot, "value.txt"), []byte(test.name), 0o600); err != nil {
				t.Fatal(err)
			}

			arguments := map[string]any{
				"program":             filepath.Join("queries", "query.fql"),
				"cwd":                 root,
				"parameters":          map[string]any{"file": "value.txt"},
				"stopOnEntry":         true,
				"clientSpecificThing": map[string]any{"ignored": true},
			}
			wantDirectory := ""

			if test.override {
				canonical, err := filepath.EvalSymlinks(runtimeRoot)
				if err != nil {
					t.Fatal(err)
				}

				wantDirectory = filepath.Clean(canonical)
				configured := runtimeRoot

				if test.symlink {
					configured = filepath.Join(t.TempDir(), "runtime-link")
					if err := os.Symlink(runtimeRoot, configured); err != nil {
						t.Skipf("create directory symlink: %v", err)
					}
				}

				arguments["workingDirectory"] = "  " + configured + string(filepath.Separator) + ".  "
			}

			var diagnostics bytes.Buffer
			client := newTestClientWithOptions(t, Options{Logger: newCaptureLogger(&diagnostics, zerolog.InfoLevel)})
			initializeDAP(t, client)
			sendDAPLaunch(t, client, arguments)

			if _, ok := client.read().(*protocol.InitializedEvent); !ok {
				t.Fatal("expected initialized event")
			}

			client.server.stateMu.Lock()
			owned := client.server.owned
			client.server.stateMu.Unlock()

			if owned.root != root || owned.program != program {
				t.Fatalf("launch root/program = %q/%q, want %q/%q", owned.root, owned.program, root, program)
			}

			snapshot, err := client.server.debugs.GetSession(t.Context(), owned.debug)
			if err != nil {
				t.Fatal(err)
			}

			if snapshot.Options.WorkingDirectory != wantDirectory {
				t.Fatalf("working directory = %q, want %q", snapshot.Options.WorkingDirectory, wantDirectory)
			}

			setBreakpoints := client.request("setBreakpoints")
			client.send(&protocol.SetBreakpointsRequest{
				Request: setBreakpoints,
				Arguments: protocol.SetBreakpointsArguments{
					Source:      protocol.Source{Path: filepath.Join("queries", "query.fql")},
					Breakpoints: []protocol.SourceBreakpoint{{Line: 2}},
				},
			})

			breakpoints, ok := client.read().(*protocol.SetBreakpointsResponse)
			if !ok || !breakpoints.Success || len(breakpoints.Body.Breakpoints) != 1 {
				t.Fatalf("setBreakpoints response = %#v", breakpoints)
			}

			breakpoint := breakpoints.Body.Breakpoints[0]
			if !breakpoint.Verified || breakpoint.Id == 0 || breakpoint.Line != 2 ||
				breakpoint.Source == nil || breakpoint.Source.Path != program {
				t.Fatalf("breakpoint = %#v, want program source at line 2", breakpoint)
			}

			completePendingDAPLaunch(t, client)
			continueDAP(t, client)

			stopped, ok := client.read().(*protocol.StoppedEvent)
			if !ok || stopped.Body.Reason != "breakpoint" || len(stopped.Body.HitBreakpointIds) != 1 ||
				stopped.Body.HitBreakpointIds[0] != breakpoint.Id {
				t.Fatalf("breakpoint stop = %#v", stopped)
			}

			stackTrace := client.request("stackTrace")
			client.send(&protocol.StackTraceRequest{
				Request:   stackTrace,
				Arguments: protocol.StackTraceArguments{ThreadId: threadID},
			})

			stack, ok := client.read().(*protocol.StackTraceResponse)
			if !ok || !stack.Success || len(stack.Body.StackFrames) != 1 {
				t.Fatalf("stackTrace response = %#v", stack)
			}

			frame := stack.Body.StackFrames[0]
			if frame.Source == nil || frame.Source.Path != program || frame.Line != 2 {
				t.Fatalf("stack frame = %#v, want program source at line 2", frame)
			}

			evaluate := client.request("evaluate")
			client.send(&protocol.EvaluateRequest{
				Request: evaluate,
				Arguments: protocol.EvaluateArguments{
					FrameId: frame.Id, Expression: "@file", Context: "repl",
				},
			})

			evaluated, ok := client.read().(*protocol.EvaluateResponse)
			if !ok || !evaluated.Success || evaluated.Body.Result != `"value.txt"` {
				t.Fatalf("parameter evaluation = %#v", evaluated)
			}

			continueDAP(t, client)

			wantOutput, err := json.Marshal(test.name)
			if err != nil {
				t.Fatal(err)
			}

			output, ok := client.read().(*protocol.OutputEvent)
			if !ok || output.Body.Category != "stdout" || output.Body.Output != string(wantOutput)+"\n" {
				t.Fatalf("output = %#v, want %s", output, wantOutput)
			}

			if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 0 {
				t.Fatalf("exited event = %#v", exited)
			}

			if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
				t.Fatal("expected terminated event")
			}

			client.disconnect()

			created := requireDiagnostic(t, decodeDiagnostics(t, diagnostics.Bytes()), diagnosticRecord{
				"message": "DAP debug session created", "program": program, "root": root,
			})

			directory, present := created["working_directory"]
			if present != test.override || (present && directory != wantDirectory) {
				t.Fatalf("logged working_directory = %#v, present = %v; want %q, %v", directory, present, wantDirectory, test.override)
			}
		})
	}
}

func TestDAPLaunchRejectsInvalidWorkingDirectoryAndAllowsRetry(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "null", value: nil, want: "working directory must be a string"},
		{name: "number", value: 42, want: "working directory must be a string"},
		{name: "object", value: map[string]any{"secret": "DO_NOT_LOG"}, want: "working directory must be a string"},
		{name: "empty", value: "", want: "working directory must not be blank"},
		{name: "whitespace", value: " \t ", want: "working directory must not be blank"},
		{name: "relative", value: "runtime", want: "working directory must be absolute"},
		{name: "missing", want: "resolve working directory"},
		{name: "file", want: "open working directory"},
		{name: "broken symlink", want: "resolve working directory"},
		{name: "symlink loop", want: "resolve working directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			program := writeDAPProgram(t, root, "RETURN 1")
			value := test.value
			switch test.name {
			case "missing":
				value = filepath.Join(t.TempDir(), "missing")
			case "file":
				value = program
			case "broken symlink", "symlink loop":
				link := filepath.Join(t.TempDir(), "runtime-link")
				target := link + "-missing"

				if test.name == "symlink loop" {
					target = link
				}

				if err := os.Symlink(target, link); err != nil {
					t.Skipf("create invalid directory symlink: %v", err)
				}

				value = link
			}

			var diagnostics bytes.Buffer
			client := newTestClientWithOptions(t, Options{Logger: newCaptureLogger(&diagnostics, zerolog.InfoLevel)})
			initializeDAP(t, client)
			sendDAPLaunch(t, client, map[string]any{
				"program": program, "cwd": root, "workingDirectory": value,
			})

			response, ok := client.read().(*protocol.ErrorResponse)
			if !ok || response.Success || response.Command != "launch" ||
				!strings.Contains(response.Message, test.want) || response.Body.Error == nil ||
				response.Body.Error.Format != response.Message {
				t.Fatalf("launch response = %#v, want working-directory failure containing %q", response, test.want)
			}

			client.server.stateMu.Lock()
			owned := client.server.owned
			launched := client.server.launched
			pending := client.server.pendingLaunch
			client.server.stateMu.Unlock()

			if owned.workspace != "" || owned.session != "" || owned.debug != "" || launched || pending != nil {
				t.Fatalf("failed launch retained state: owned=%+v launched=%v pending=%+v", owned, launched, pending)
			}

			workspaces, err := client.server.workspaces.List(t.Context())
			if err != nil || len(workspaces) != 0 {
				t.Fatalf("workspaces after failed launch = %v, err = %v", workspaces, err)
			}

			// Failure diagnostics precede the response; no watch or successful launch can log yet.
			records := decodeDiagnostics(t, diagnostics.Bytes())
			failure := requireDiagnostic(t, records, diagnosticRecord{
				"message": "DAP request failed", "command": "launch", "program": program, "root": root,
			})
			directory, present := failure["working_directory"]

			wantDirectory, suppliedString := value.(string)
			if present != suppliedString || (present && directory != wantDirectory) {
				t.Fatalf("logged working_directory = %#v, present = %v; want %q, %v", directory, present, wantDirectory, suppliedString)
			}

			if strings.Contains(diagnostics.String(), "DO_NOT_LOG") {
				t.Fatal("diagnostics contain invalid working-directory object contents")
			}

			if sessionID, ok := failure["execution_session_id"].(string); ok {
				_, err := client.server.executions.GetSession(t.Context(), exec.SessionID(sessionID))
				if !errors.Is(err, exec.ErrSessionNotFound) {
					t.Fatalf("failed launch execution session error = %v, want ErrSessionNotFound", err)
				}
			}

			launchDAP(t, client, program, root, true)
			completePendingDAPLaunch(t, client)
			client.disconnect()
		})
	}
}

func sendDAPLaunch(t *testing.T, client *testClient, arguments map[string]any) {
	t.Helper()

	body, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}

	launch := client.request("launch")
	client.send(&protocol.LaunchRequest{Request: launch, Arguments: body})
}

func continueDAP(t *testing.T, client *testClient) {
	t.Helper()

	request := client.request("continue")
	client.send(&protocol.ContinueRequest{
		Request: request, Arguments: protocol.ContinueArguments{ThreadId: threadID},
	})

	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("continue response = %#v", response)
	}
}
