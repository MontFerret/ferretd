package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDAPProcessUnicodeCoordinatesAndSnapshot(t *testing.T) {
	binary := buildDAPProcess(t)

	tests := []struct {
		name        string
		text        string
		requestLine int
		column      int
		line        int
	}{
		{"same line emoji", "LET text = \"😀\" RETURN text\n", 1, 17, 1},
		{"preceding emoji CRLF relocation", "LET text = \"😀\"\r\n\r\nRETURN text\r\n", 2, 1, 3},
		{"malformed and supplementary", "LET text = \"é\x80😀\xf0\x9f\" RETURN text\n", 1, 21, 1},
	}
	for _, test := range tests {
		for _, lineBase := range []int{0, 1} {
			for _, columnBase := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s/lines%d/columns%d", test.name, lineBase, columnBase), func(t *testing.T) {
					root := t.TempDir()

					path := filepath.Join(root, "query.fql")
					if err := os.WriteFile(path, []byte(test.text), 0o600); err != nil {
						t.Fatal(err)
					}

					client := newDAPProcessClient(t, binary)
					client.send("initialize", map[string]any{
						"adapterID": "ferretd", "linesStartAt1": lineBase == 1, "columnsStartAt1": columnBase == 1,
					})
					client.response("initialize")
					client.send("launch", map[string]any{"program": path, "cwd": root, "stopOnEntry": true})
					client.event("initialized")

					// Assert actual wire presence at the first source character, especially
					// the zero-based values omitted by go-dap's standard Breakpoint encoding.
					client.send("setBreakpoints", map[string]any{
						"source":      map[string]any{"path": path},
						"breakpoints": []map[string]any{{"line": lineBase, "column": columnBase}},
					})
					first := client.response("setBreakpoints")

					var coordinates struct {
						Breakpoints []struct {
							Line     *int `json:"line"`
							Column   *int `json:"column"`
							Verified bool `json:"verified"`
							ID       int  `json:"id"`
						} `json:"breakpoints"`
					}
					if err := json.Unmarshal(first.Body, &coordinates); err != nil {
						t.Fatal(err)
					}

					if len(coordinates.Breakpoints) != 1 {
						t.Fatalf("first breakpoint: %s", first.Body)
					}

					origin := coordinates.Breakpoints[0]
					if !origin.Verified || origin.Line == nil || *origin.Line != lineBase || origin.Column == nil || *origin.Column != columnBase {
						t.Fatalf("first-character wire coordinates: %s", first.Body)
					}

					// A valid trailing blank line stays unbound and has no invented column.
					lastLine := strings.Count(test.text, "\n") + lineBase
					client.send("setBreakpoints", map[string]any{
						"source":      map[string]any{"path": path},
						"breakpoints": []map[string]any{{"line": lastLine}},
					})
					unbound := client.response("setBreakpoints")
					coordinates.Breakpoints = nil

					if err := json.Unmarshal(unbound.Body, &coordinates); err != nil {
						t.Fatal(err)
					}

					if len(coordinates.Breakpoints) != 1 || coordinates.Breakpoints[0].Verified ||
						coordinates.Breakpoints[0].Line == nil || *coordinates.Breakpoints[0].Line != lastLine ||
						coordinates.Breakpoints[0].Column != nil {
						t.Fatalf("unbound line-only wire coordinates: %s", unbound.Body)
					}

					if err := os.WriteFile(path, []byte("RETURN 0"), 0o600); err != nil {
						t.Fatal(err)
					}

					target := map[string]any{"line": test.requestLine - 1 + lineBase}
					if test.requestLine == test.line {
						target["column"] = test.column - 1 + columnBase
					}

					client.send("setBreakpoints", map[string]any{
						"source": map[string]any{"path": path}, "breakpoints": []map[string]any{target},
					})

					replaced := client.response("setBreakpoints")
					if err := json.Unmarshal(replaced.Body, &coordinates); err != nil {
						t.Fatal(err)
					}

					if len(coordinates.Breakpoints) != 1 {
						t.Fatalf("replacement: %s", replaced.Body)
					}

					breakpoint := coordinates.Breakpoints[0]
					if !breakpoint.Verified || breakpoint.Line == nil || *breakpoint.Line != test.line-1+lineBase ||
						breakpoint.Column == nil || *breakpoint.Column != test.column-1+columnBase {
						t.Fatalf("resolved wire coordinates: %s", replaced.Body)
					}

					client.send("configurationDone", map[string]any{})
					client.response("configurationDone")
					client.response("launch")
					client.event("stopped")
					client.send("continue", map[string]any{"threadId": 1})
					client.response("continue")
					stopped := client.event("stopped")

					var stop struct {
						Reason           string `json:"reason"`
						HitBreakpointIDs []int  `json:"hitBreakpointIds"`
					}
					if err := json.Unmarshal(stopped.Body, &stop); err != nil {
						t.Fatal(err)
					}

					if stop.Reason != "breakpoint" || len(stop.HitBreakpointIDs) != 1 || stop.HitBreakpointIDs[0] != breakpoint.ID {
						t.Fatalf("breakpoint stop: %s", stopped.Body)
					}

					client.send("stackTrace", map[string]any{"threadId": 1})
					frames := client.response("stackTrace")

					var stack struct {
						Frames []struct {
							Line   int `json:"line"`
							Column int `json:"column"`
						} `json:"stackFrames"`
					}
					if err := json.Unmarshal(frames.Body, &stack); err != nil {
						t.Fatal(err)
					}

					if len(stack.Frames) != 1 || stack.Frames[0].Line != *breakpoint.Line || stack.Frames[0].Column != *breakpoint.Column {
						t.Fatalf("stack frame coordinates: %s", frames.Body)
					}

					client.disconnect()
				})
			}
		}
	}
}
