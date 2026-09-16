package dap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	apidebugger "github.com/MontFerret/api/debugger"
	apisource "github.com/MontFerret/api/source"
	"github.com/MontFerret/ferret/v2/uapi"

	protocol "github.com/google/go-dap"
)

func TestDAPUnicodePositionsUseCompiledSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		requestedLine  int
		column         int
		resolvedLine   int
		resolvedColumn int
	}{
		{"same line emoji", "LET text = \"😀\" RETURN text", 1, 17, 1, 17},
		{"preceding emoji", "LET text = \"😀\"\nLET value = 42\nRETURN value", 2, 0, 2, 1},
		{"relocated CRLF", "LET text = \"😀\"\r\n\r\n  RETURN text\r\n", 2, 0, 3, 3},
		{"combining tab", "LET text = \"e\u0301\"\tRETURN text", 1, 17, 1, 17},
		{"malformed and supplementary", "LET text = \"é\x80😀\xf0\x9f\" RETURN text", 1, 21, 1, 21},
	}
	for _, test := range tests {
		for _, lineBase := range []int{0, 1} {
			for _, columnBase := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s/lines%d/columns%d", test.name, lineBase, columnBase), func(t *testing.T) {
					client := newTestClient(t)
					root := t.TempDir()
					program := writeDAPProgram(t, root, test.text)
					client.sendRawRequest("initialize", fmt.Sprintf(
						`{"adapterID":"ferretd","linesStartAt1":%t,"columnsStartAt1":%t}`, lineBase == 1, columnBase == 1))

					if response, ok := client.read().(*protocol.InitializeResponse); !ok || !response.Success {
						t.Fatalf("initialize = %#v", response)
					}

					launchDAP(t, client, program, root, true)
					// The new contents have neither the old lines nor its column boundaries.
					if err := os.WriteFile(program, []byte("RETURN 0"), 0o600); err != nil {
						t.Fatal(err)
					}

					column := ""
					if test.column != 0 {
						// One unit after RETURN must not bind backwards to RETURN. This proves
						// request translation independently of the resolved response position.
						client.sendRawRequest("setBreakpoints", fmt.Sprintf(
							`{"source":{"path":%q},"breakpoints":[{"line":%d,"column":%d}]}`,
							program, test.requestedLine-1+lineBase, test.column+columnBase))

						response, ok := client.read().(*protocol.SetBreakpointsResponse)
						if !ok || !response.Success || len(response.Body.Breakpoints) != 1 || response.Body.Breakpoints[0].Verified ||
							response.Body.Breakpoints[0].Line != test.requestedLine-1+lineBase ||
							response.Body.Breakpoints[0].Column != test.column+columnBase {
							t.Fatalf("position after executable start = %#v", response)
						}

						column = fmt.Sprintf(`,"column":%d`, test.column-1+columnBase)
					}

					client.sendRawRequest("setBreakpoints", fmt.Sprintf(
						`{"source":{"path":%q},"breakpoints":[{"line":%d%s}]}`,
						program, test.requestedLine-1+lineBase, column))

					response, ok := client.read().(*protocol.SetBreakpointsResponse)
					if !ok || !response.Success || len(response.Body.Breakpoints) != 1 {
						t.Fatalf("setBreakpoints = %#v", response)
					}

					breakpoint := response.Body.Breakpoints[0]
					if !breakpoint.Verified || breakpoint.Line != test.resolvedLine-1+lineBase || breakpoint.Column != test.resolvedColumn-1+columnBase {
						t.Fatalf("resolved breakpoint = %+v", breakpoint)
					}

					completePendingDAPLaunch(t, client)

					if err := os.WriteFile(program, []byte("LET changed = \"😀😀😀\"\nRETURN changed"), 0o600); err != nil {
						t.Fatal(err)
					}

					continueDAP(t, client)

					stopped, ok := client.read().(*protocol.StoppedEvent)
					if !ok || stopped.Body.Reason != stopReasonBreakpoint || len(stopped.Body.HitBreakpointIds) != 1 || stopped.Body.HitBreakpointIds[0] != breakpoint.Id {
						t.Fatalf("stop = %#v", stopped)
					}

					client.send(&protocol.StackTraceRequest{
						Request:   client.request("stackTrace"),
						Arguments: protocol.StackTraceArguments{ThreadId: threadID},
					})

					frames, ok := client.read().(*protocol.StackTraceResponse)
					if !ok || !frames.Success || len(frames.Body.StackFrames) != 1 {
						t.Fatalf("frames = %#v", frames)
					}

					frame := frames.Body.StackFrames[0]
					if frame.Line != breakpoint.Line || frame.Column != breakpoint.Column {
						t.Fatalf("frame = %+v, breakpoint = %+v", frame, breakpoint)
					}

					snapshot, err := client.server.executions.GetSession(t.Context(), client.server.owned.session)
					if err != nil || snapshot.Text != test.text {
						t.Fatalf("retained source changed, error = %v", err)
					}

					client.disconnect()
				})
			}
		}
	}
}

func TestDAPColumnPresenceAndInvalidReplacement(t *testing.T) {
	for _, base := range []int{0, 1} {
		t.Run(fmt.Sprintf("base%d", base), func(t *testing.T) {
			client := newTestClient(t)
			root := t.TempDir()
			program := writeDAPProgram(t, root, "LET text = \"😀\"\nRETURN text\n")
			client.sendRawRequest("initialize", fmt.Sprintf(
				`{"adapterID":"ferretd","linesStartAt1":%t,"columnsStartAt1":%t}`, base == 1, base == 1))

			if response, ok := client.read().(*protocol.InitializeResponse); !ok || !response.Success {
				t.Fatalf("initialize = %#v", response)
			}

			launchDAP(t, client, program, root, true)
			client.sendRawRequest("setBreakpoints", fmt.Sprintf(
				`{"source":{"path":%q},"breakpoints":[{"line":%d},{"line":%d,"column":%d}]}`,
				program, base+1, base+1, base))

			response, ok := client.read().(*protocol.SetBreakpointsResponse)
			if !ok || !response.Success || len(response.Body.Breakpoints) != 2 || response.Body.Breakpoints[0].Id == response.Body.Breakpoints[1].Id {
				t.Fatalf("line-only and explicit first column = %#v", response)
			}

			want := response.Body.Breakpoints
			for _, invalid := range []string{
				fmt.Sprintf(`{"line":%d}`, base-1),
				fmt.Sprintf(`{"line":%d}`, base+3),
				fmt.Sprintf(`{"line":%d,"column":-1}`, base),
				fmt.Sprintf(`{"line":%d,"column":%d}`, base, base+13), // inside emoji
				fmt.Sprintf(`{"line":%d,"column":100}`, base),
				fmt.Sprintf(`{"line":%d,"column":null}`, base),
			} {
				client.sendRawRequest("setBreakpoints", fmt.Sprintf(
					`{"source":{"path":%q},"breakpoints":[{"line":%d},%s]}`, program, base, invalid))

				if failure, ok := client.read().(*protocol.ErrorResponse); !ok || failure.Success {
					t.Fatalf("invalid %s = %#v", invalid, failure)
				}
			}

			if base == 1 {
				client.sendRawRequest("setBreakpoints", fmt.Sprintf(
					`{"source":{"path":%q},"breakpoints":[{"line":1,"column":0}]}`, program))

				if failure, ok := client.read().(*protocol.ErrorResponse); !ok || failure.Success {
					t.Fatalf("one-based zero column = %#v", failure)
				}
			}

			completePendingDAPLaunch(t, client)
			continueDAP(t, client)

			stopped, ok := client.read().(*protocol.StoppedEvent)
			if !ok || stopped.Body.Reason != stopReasonBreakpoint || len(stopped.Body.HitBreakpointIds) != 2 {
				t.Fatalf("preserved breakpoint stop = %#v", stopped)
			}

			for _, breakpoint := range want {
				found := false
				for _, id := range stopped.Body.HitBreakpointIds {
					found = found || id == breakpoint.Id
				}

				if !found {
					t.Fatalf("lost breakpoint %d: %+v", breakpoint.Id, stopped.Body)
				}
			}

			client.disconnect()
		})
	}
}

func TestDAPBreakpointJSONPreservesZeroAndOmittedCoordinates(t *testing.T) {
	for _, column := range []*int{nil, new(int)} {
		response := &sourceBreakpointsResponse{}
		response.Body.Breakpoints = []sourceBreakpoint{{Line: new(int), Column: column}}

		content, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(string(content), `"line":0`) || strings.Contains(string(content), `"column":0`) != (column != nil) {
			t.Fatalf("coordinate presence: %s", content)
		}
	}
}

func TestDAPUnicodeExceptionFramePosition(t *testing.T) {
	client := newTestClient(t)
	root := t.TempDir()
	program := writeDAPProgram(t, root, "LET text = \"😀\" RETURN 1 / @zero")
	initializeDAP(t, client)
	client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q,"cwd":%q,"stopOnEntry":true,"parameters":{"zero":0}}`, program, root))

	if _, ok := client.read().(*protocol.InitializedEvent); !ok {
		t.Fatal("missing initialized")
	}

	completePendingDAPLaunch(t, client)
	continueDAP(t, client)

	if stopped, ok := client.read().(*protocol.StoppedEvent); !ok || stopped.Body.Reason != "exception" {
		t.Fatalf("exception stop = %#v", stopped)
	}

	client.send(&protocol.StackTraceRequest{Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID}})

	frames, ok := client.read().(*protocol.StackTraceResponse)
	if !ok || !frames.Success || len(frames.Body.StackFrames) != 1 || frames.Body.StackFrames[0].Column != 17 {
		t.Fatalf("exception frames = %#v", frames)
	}

	client.disconnect()
}

func TestDAPRejectsInvalidNativePositions(t *testing.T) {
	for _, position := range []apisource.Position{
		{Line: 1, Column: 14}, // inside a valid emoji encoding
		{Line: 1, Column: 0},
		{Line: 1, Column: 100},
		{Line: 2, Column: 1},
	} {
		t.Run(fmt.Sprint(position), func(t *testing.T) {
			native, err := uapi.New()
			if err != nil {
				t.Fatal(err)
			}

			d := &breakpointDebugger{}
			d.replaceFn = func(ctx context.Context, source string, requests []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
				result, err := d.Session.ReplaceBreakpoints(ctx, source, requests)
				if err == nil && len(result) != 0 {
					result[0].Location.Position = position
				}

				return result, err
			}
			d.framesFn = func(ctx context.Context) ([]apidebugger.Frame, error) {
				frames, err := d.Session.Frames(ctx)
				if err == nil && len(frames) != 0 {
					frames[0].Location.Position = position
				}

				return frames, err
			}

			client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: d})
			root := t.TempDir()
			program := writeDAPProgram(t, root, "LET text = \"😀\" RETURN text")
			initializeDAP(t, client)
			launchDAP(t, client, program, root, true)
			client.sendRawRequest("setBreakpoints", fmt.Sprintf(
				`{"source":{"path":%q},"breakpoints":[{"line":1}]}`, program))

			if failure, ok := client.read().(*protocol.ErrorResponse); !ok || failure.Success {
				t.Fatalf("invalid native breakpoint position: %#v", failure)
			}

			completePendingDAPLaunch(t, client)
			client.send(&protocol.StackTraceRequest{Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID}})

			if failure, ok := client.read().(*protocol.ErrorResponse); !ok || failure.Success {
				t.Fatalf("invalid native frame position: %#v", failure)
			}

			client.disconnect()
		})
	}
}
