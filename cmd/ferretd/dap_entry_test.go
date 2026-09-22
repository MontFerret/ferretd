package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	protocol "github.com/google/go-dap"
)

func TestDAPProcessInitialStop(t *testing.T) {
	binary := buildDAPProcess(t)

	for _, test := range []struct {
		name        string
		stopOnEntry bool
		breakpoint  int
		reason      string
		line        int
	}{
		{name: "suppressed entry"},
		{name: "first statement breakpoint", breakpoint: 1, reason: "breakpoint", line: 1},
		{name: "entry", stopOnEntry: true, reason: "entry", line: 1},
		{name: "entry and breakpoint are one stop", stopOnEntry: true, breakpoint: 1, reason: "breakpoint", line: 1},
		{name: "later breakpoint", breakpoint: 2, reason: "breakpoint", line: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			program := filepath.Join(root, "query.fql")

			if err := os.WriteFile(program, []byte("LET value = 1\nRETURN value\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			client := newDAPProcessClient(t, binary)
			client.send("initialize", map[string]any{"adapterID": "ferretd"})
			client.response("initialize")
			client.send("launch", map[string]any{"program": program, "cwd": root, "stopOnEntry": test.stopOnEntry})
			client.event("initialized")
			var wantIDs []int

			if test.breakpoint != 0 {
				client.send("setBreakpoints", map[string]any{
					"source": map[string]any{"path": program}, "breakpoints": []map[string]int{{"line": test.breakpoint}},
				})
				response := client.response("setBreakpoints")
				var body protocol.SetBreakpointsResponseBody

				if err := json.Unmarshal(response.Body, &body); err != nil {
					t.Fatal(err)
				}

				if len(body.Breakpoints) != 1 {
					t.Fatalf("breakpoints = %+v", body)
				}

				breakpoint := body.Breakpoints[0]
				if !breakpoint.Verified || breakpoint.Id == 0 || breakpoint.Line != test.breakpoint || breakpoint.Column != 1 {
					t.Fatalf("breakpoint = %+v", breakpoint)
				}

				wantIDs = []int{breakpoint.Id}
			}

			client.send("configurationDone", map[string]any{})
			client.response("configurationDone")
			client.response("launch")
			wantStops := 0

			if test.reason != "" {
				wantStops = 1
				stopped := client.event("stopped")
				var stop protocol.StoppedEventBody

				if err := json.Unmarshal(stopped.Body, &stop); err != nil {
					t.Fatal(err)
				}

				if stop.Reason != test.reason || !slices.Equal(stop.HitBreakpointIds, wantIDs) {
					t.Fatalf("stop = %+v, want %s with IDs %v", stop, test.reason, wantIDs)
				}

				client.send("stackTrace", map[string]any{"threadId": 1})
				response := client.response("stackTrace")
				var stack protocol.StackTraceResponseBody

				if err := json.Unmarshal(response.Body, &stack); err != nil {
					t.Fatal(err)
				}

				if len(stack.StackFrames) != 1 {
					t.Fatalf("stack = %+v", stack)
				}

				frame := stack.StackFrames[0]
				if frame.Source == nil || frame.Source.Path != program || frame.Line != test.line || frame.Column != 1 {
					t.Fatalf("frame = %+v, want %s:%d:1", frame, program, test.line)
				}

				client.send("continue", map[string]any{"threadId": 1})
				client.response("continue")
			}

			output := client.event("output")
			var result protocol.OutputEventBody

			if err := json.Unmarshal(output.Body, &result); err != nil {
				t.Fatal(err)
			}

			if result.Category != "stdout" || result.Output != "1\n" {
				t.Fatalf("output = %+v", result)
			}

			exited := client.event("exited")
			var exit protocol.ExitedEventBody

			if err := json.Unmarshal(exited.Body, &exit); err != nil {
				t.Fatal(err)
			}

			if exit.ExitCode != 0 {
				t.Fatalf("exit = %+v", exit)
			}

			client.event("terminated")
			client.disconnect()

			// Count the complete protocol transcript, including after continue,
			// rather than using a timeout to guess whether another stop follows.
			if client.stoppedEvents != wantStops {
				t.Fatalf("stopped events = %d, want %d", client.stoppedEvents, wantStops)
			}
		})
	}
}
