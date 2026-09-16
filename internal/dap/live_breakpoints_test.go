package dap

import (
	"testing"
	"time"

	protocol "github.com/google/go-dap"
)

func TestDAPLiveBreakpointMutations(t *testing.T) {
	for _, test := range []struct {
		name    string
		initial []int
		next    []int
	}{
		{name: "add", next: []int{4}},
		{name: "remove", initial: []int{4, 5}, next: []int{5}},
		{name: "replace", initial: []int{4}, next: []int{5}},
		{name: "clear", initial: []int{4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLiveBreakpointFixture(t, true, test.initial...)
			f.continueExecution()
			f.waitVisit()
			bound := f.replace(test.next...)

			for index, breakpoint := range bound {
				if !breakpoint.Verified || breakpoint.Line != test.next[index] || breakpoint.Id == 0 {
					t.Fatalf("bound = %+v", bound)
				}
			}

			f.advance()

			if len(bound) != 0 {
				f.stopped(stopReasonBreakpoint, bound[0].Id)
				f.assertMappings(len(bound), 0, 0)
				f.replace()
				f.continueExecution()
			}

			// Reaching both later visits proves removed locations do not stop.
			f.waitVisit()
			f.advance()
			f.waitVisit()
			f.advance()

			if output, ok := f.client.read().(*protocol.OutputEvent); !ok || output.Body.Output != "[3,4,5]\n" {
				t.Fatalf("completion output = %#v", output)
			}

			if exited, ok := f.client.read().(*protocol.ExitedEvent); !ok || exited.Body.ExitCode != 0 {
				t.Fatalf("exit = %#v", exited)
			}

			if _, ok := f.client.read().(*protocol.TerminatedEvent); !ok {
				t.Fatal("missing termination")
			}

			f.assertMappings(0, 0, 0)
			f.client.disconnect()
		})
	}
}

func TestDAPLiveReplacementRequestOrderAndBinding(t *testing.T) {
	f := newLiveBreakpointFixture(t, true)
	f.continueExecution()
	f.waitVisit()
	requests := []*protocol.SetBreakpointsRequest{f.replacement(4), f.replacement(5), f.replacement(3, 99)}
	sent := make(chan error, 1)
	go func() {
		for _, request := range requests {
			if err := protocol.WriteProtocolMessage(f.client.input, request); err != nil {
				sent <- err

				return
			}
		}

		sent <- nil
	}()

	var final []protocol.Breakpoint
	for _, request := range requests {
		response, ok := f.client.read().(*protocol.SetBreakpointsResponse)
		if !ok || !response.Success || response.RequestSeq != request.Seq {
			t.Fatalf("ordered response = %#v, want request %d", response, request.Seq)
		}

		final = response.Body.Breakpoints
	}

	select {
	case err := <-sent:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipelined writer did not finish")
	}

	if len(final) != 2 || !final[0].Verified || final[0].Line != 4 || final[1].Verified {
		t.Fatalf("relocated/unverified response = %+v", final)
	}

	repeated := f.replace(3, 99)
	if repeated[0].Id != final[0].Id || repeated[1].Id != final[1].Id {
		t.Fatalf("unchanged IDs = %+v, want %+v", repeated, final)
	}

	f.replace()

	readded := f.replace(3, 99)
	if readded[0].Id != final[0].Id || readded[1].Id != final[1].Id {
		t.Fatalf("re-added requested IDs = %+v, want %+v", readded, final)
	}

	f.advance()
	f.stopped(stopReasonBreakpoint, final[0].Id)
	f.assertMappings(2, 0, 0)
	f.client.disconnect()
}

func TestDAPLiveMutationPauseStepAndShutdown(t *testing.T) {
	for _, command := range []string{"terminate", "disconnect"} {
		t.Run(command, func(t *testing.T) {
			// Exercise the implicit continuation after a suppressed entry stop.
			f := newLiveBreakpointFixture(t, false, 4)
			f.waitVisit()
			f.replace(5)
			f.replace()
			f.client.send(&protocol.PauseRequest{Request: f.client.request("pause"), Arguments: protocol.PauseArguments{ThreadId: threadID}})

			if response, ok := f.client.read().(*protocol.PauseResponse); !ok || !response.Success {
				t.Fatalf("pause = %#v", response)
			}

			f.advance()
			f.stopped(stopReasonPause, 0)
			f.assertMappings(0, 0, 0)
			f.client.send(&protocol.NextRequest{Request: f.client.request("next"), Arguments: protocol.NextArguments{ThreadId: threadID}})

			if response, ok := f.client.read().(*protocol.NextResponse); !ok || !response.Success {
				t.Fatalf("next = %#v", response)
			}

			f.stopped(stopReasonStep, 0)
			f.continueExecution()
			f.waitVisit()
			f.replace(4)
			f.replace()

			if command == "terminate" {
				f.client.send(&protocol.TerminateRequest{Request: f.client.request("terminate")})

				if response, ok := f.client.read().(*protocol.TerminateResponse); !ok || !response.Success {
					t.Fatalf("terminate = %#v", response)
				}

				if _, ok := f.client.read().(*protocol.TerminatedEvent); !ok {
					t.Fatal("missing termination")
				}

				f.assertMappings(0, 0, 0)
			}

			f.client.disconnect()
		})
	}
}

func TestDAPLiveBreakpointSessionIsolation(t *testing.T) {
	a := newLiveBreakpointFixture(t, true, 4)
	b := newLiveBreakpointFixture(t, true, 4)
	want := b.replace(4)[0].Id
	a.continueExecution()
	b.continueExecution()
	a.waitVisit()
	b.waitVisit()
	a.replace()
	a.advance()
	a.waitVisit()
	b.advance()
	b.stopped(stopReasonBreakpoint, want)
	a.client.disconnect()
	b.client.disconnect()
}

func TestDAPInvalidLiveBreakpointsLeavePreviousSet(t *testing.T) {
	f := newLiveBreakpointFixture(t, true, 4)
	want := f.replace(4)[0].Id
	f.continueExecution()
	f.waitVisit()

	for _, invalid := range []protocol.SourceBreakpoint{
		{Line: 0},
		{Line: 5, Column: -1},
		{Line: 5, Condition: "true"},
		{Line: 5, HitCondition: "2"},
		{Line: 5, LogMessage: "message"},
	} {
		request := f.replacement(5)
		request.Arguments.Breakpoints = append(request.Arguments.Breakpoints, invalid)
		f.client.send(request)

		response, ok := f.client.read().(*protocol.ErrorResponse)
		if !ok || response.Success {
			t.Fatalf("invalid breakpoint %+v: %#v", invalid, response)
		}

		f.assertMappings(1, 0, 1)
	}

	f.advance()
	f.stopped(stopReasonBreakpoint, want)
	f.client.disconnect()
}
