package dap

import (
	"context"
	"errors"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/api"
	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/debug"
)

func TestDAPReplacementOrdersResponseAndPreservesConcurrentHitIDs(t *testing.T) {
	for _, oldHit := range []bool{false, true} {
		name := "new hit"

		if oldHit {
			name = "already decided hit"
		}

		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(cancel)

			native, err := uapi.New()
			if err != nil {
				t.Fatal(err)
			}

			d := &breakpointDebugger{}
			gate, captured := make(chan struct{}), make(chan struct{})
			d.continueFn = func(runCtx context.Context) (*apidebugger.Event, error) {
				if !oldHit {
					close(captured)
					select {
					case <-gate:
					case <-runCtx.Done():
						return nil, runCtx.Err()
					}
				}

				event, err := d.Session.Continue(runCtx)

				if oldHit {
					close(captured)
					select {
					case <-gate:
					case <-runCtx.Done():
						return nil, runCtx.Err()
					}
				}

				return event, err
			}

			client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: d})
			root := t.TempDir()
			program := writeDAPProgram(t, root, "LET x = 1\nLET y = 2\nRETURN x + y")
			initializeDAP(t, client)
			launchDAP(t, client, program, root, true)
			set := func(line int) {
				client.send(&protocol.SetBreakpointsRequest{
					Request: client.request("setBreakpoints"),
					Arguments: protocol.SetBreakpointsArguments{
						Source:      protocol.Source{Path: program},
						Breakpoints: []protocol.SourceBreakpoint{{Line: line}},
					},
				})
			}
			set(2)

			first, ok := client.read().(*protocol.SetBreakpointsResponse)
			if !ok || !first.Success || len(first.Body.Breakpoints) != 1 {
				t.Fatalf("first replacement = %#v", first)
			}

			oldID := first.Body.Breakpoints[0].Id
			completePendingDAPLaunch(t, client)

			subscription, err := client.server.debugs.WatchSession(ctx, client.server.owned.debug)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(subscription.Cancel)
			d.replaceFn = func(requestCtx context.Context, source string, requests []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
				bound, err := d.Session.ReplaceBreakpoints(requestCtx, source, requests)
				if err != nil {
					return nil, err
				}

				close(gate)
				for {
					select {
					case event, ok := <-subscription.Events:
						if !ok {
							return nil, errors.New("debug watch closed before stop")
						}

						if event.Snapshot.State == debug.StateStopped {
							return bound, nil
						}
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
			client.send(&protocol.ContinueRequest{Request: client.request("continue"), Arguments: protocol.ContinueArguments{ThreadId: threadID}})

			if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
				t.Fatalf("continue = %#v", response)
			}

			select {
			case <-captured:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			set(3)

			replaced, ok := client.read().(*protocol.SetBreakpointsResponse)
			if !ok || !replaced.Success || len(replaced.Body.Breakpoints) != 1 {
				t.Fatalf("replacement must precede stop: %#v", replaced)
			}

			wantID := replaced.Body.Breakpoints[0].Id

			if oldHit {
				wantID = oldID
			}

			stopped, ok := client.read().(*protocol.StoppedEvent)
			if !ok || wantID == 0 || len(stopped.Body.HitBreakpointIds) != 1 || stopped.Body.HitBreakpointIds[0] != wantID {
				t.Fatalf("concurrent stop = %#v, want ID %d", stopped, wantID)
			}

			client.disconnect()
		})
	}
}

func TestDAPEmitsAvailableOutputBeforeCommandFailure(t *testing.T) {
	native, err := uapi.New()
	if err != nil {
		t.Fatal(err)
	}

	d := &breakpointDebugger{continueFn: func(context.Context) (*apidebugger.Event, error) {
		return &apidebugger.Event{Reason: apidebugger.ReasonCompleted, Output: &api.Output{Content: []byte("42")}}, errors.New("completion cleanup")
	}}
	client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: d})
	root := t.TempDir()
	program := writeDAPProgram(t, root, "RETURN 42")
	initializeDAP(t, client)
	launchDAP(t, client, program, root, true)
	completePendingDAPLaunch(t, client)
	client.send(&protocol.ContinueRequest{Request: client.request("continue"), Arguments: protocol.ContinueArguments{ThreadId: threadID}})

	if response, ok := client.read().(*protocol.ContinueResponse); !ok || !response.Success {
		t.Fatalf("continue = %#v", response)
	}

	output, ok := client.read().(*protocol.OutputEvent)
	if !ok || output.Body.Category != outputCategoryStdout || output.Body.Output != "42\n" {
		t.Fatalf("available output = %#v", output)
	}

	failure, ok := client.read().(*protocol.OutputEvent)
	if !ok || failure.Body.Category != outputCategoryStderr || failure.Body.Output != "completion cleanup\n" {
		t.Fatalf("failure output = %#v", failure)
	}

	if exited, ok := client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 1 {
		t.Fatalf("exit = %#v", exited)
	}

	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("missing terminated event")
	}

	client.disconnect()
}
