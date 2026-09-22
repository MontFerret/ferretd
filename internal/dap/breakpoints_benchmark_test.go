package dap

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/exec"
)

func BenchmarkDAPBreakpointReplacement(b *testing.B) {
	for _, name := range []string{"unchanged", "alternating", "clear_readd"} {
		b.Run(name, func(b *testing.B) {
			server, program := benchmarkBreakpointServer(b)
			request := &sourceBreakpointsRequest{SetBreakpointsRequest: protocol.SetBreakpointsRequest{
				Request: protocol.Request{Command: "setBreakpoints"},
				Arguments: protocol.SetBreakpointsArguments{
					Source: protocol.Source{Path: program},
				},
			}}
			sets := [][]protocol.SourceBreakpoint{{{Line: 2}}, {{Line: 2}}}

			switch name {
			case "alternating":
				sets[1] = []protocol.SourceBreakpoint{{Line: 3}}
			case "clear_readd":
				sets[1] = nil
			}

			request.Arguments.Breakpoints = sets[0]
			if err := server.handleSetBreakpoints(b.Context(), request); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			index := 0

			for b.Loop() {
				request.Arguments.Breakpoints = sets[index%len(sets)]
				if err := server.handleSetBreakpoints(b.Context(), request); err != nil {
					b.Fatal(err)
				}

				index++
			}
		})
	}
}

func benchmarkBreakpointServer(b *testing.B) (*Server, string) {
	b.Helper()
	root := b.TempDir()
	program := filepath.Join(root, "query.fql")

	if err := os.WriteFile(program, []byte("LET x = 1\nLET y = 2\nRETURN x + y"), 0o600); err != nil {
		b.Fatal(err)
	}

	runtime, err := uapi.New()
	if err != nil {
		b.Fatal(err)
	}

	server, err := newServer(strings.NewReader(""), io.Discard, Options{}.normalized(), runtime)
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = server.cleanup() })
	ctx := context.Background()

	workspace, err := server.workspaces.Open(ctx, root)
	if err != nil {
		b.Fatal(err)
	}

	server.owned.workspace = workspace.ID()

	session, err := server.executions.CreateSession(ctx, workspace.ID(), "query.fql")
	if err != nil {
		b.Fatal(err)
	}

	server.owned.session = session.ID
	server.owned.coordinates = newSourceCoordinates(session.Text, server.client)

	debug, err := server.debugs.CreateSession(ctx, session.ID, nil, exec.RuntimeOptions{})
	if err != nil {
		b.Fatal(err)
	}

	server.owned.debug = debug.ID
	server.owned.root = root
	server.owned.program = program

	server.owned.programIdentity, err = newSourceIdentity(program, root)
	if err != nil {
		b.Fatal(err)
	}

	server.launched = true
	server.initialStop = &initialStop{}

	return server, program
}
