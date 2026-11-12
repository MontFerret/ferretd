package dap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2/uapi"
)

func TestDAPCancelPreparationAndRetry(t *testing.T) {
	for _, stage := range []string{"compile", "debug compile", "debugger"} {
		t.Run(stage, func(t *testing.T) {
			client, entered, settled := newPreparationClient(t, stage, nil)
			initializeDAP(t, client)
			program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
			launch := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q}`, program))
			awaitDAPSignal(t, entered)
			client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})
			requireCancelledLaunch(t, client, launch.Seq)

			if response, ok := client.read().(*protocol.CancelResponse); !ok || !response.Success {
				t.Fatalf("cancel acknowledgment = %#v", response)
			}

			if settled.Load() != 1 {
				t.Fatalf("late constructor settlement count = %d", settled.Load())
			}

			sendDAPLaunch(t, client, map[string]any{"program": program, "stopOnEntry": true})

			if _, ok := client.read().(*protocol.InitializedEvent); !ok {
				t.Fatal("early cancellation did not allow retry")
			}

			completePendingDAPLaunch(t, client)
			client.disconnect()
		})
	}
}

func requireCancelledLaunch(t *testing.T, client *testClient, sequence int) *protocol.ErrorResponse {
	t.Helper()

	response, ok := client.read().(*protocol.ErrorResponse)
	if !ok || response.Success || response.Command != "launch" || response.RequestSeq != sequence || response.Message != "cancelled" {
		t.Fatalf("cancelled launch = %#v", response)
	}

	return response
}

func TestDAPCancellationRetainsRollbackErrors(t *testing.T) {
	cleanupErr := errors.New("test plan cleanup failed")
	client, entered, _ := newPreparationClient(t, "compile", cleanupErr)
	initializeDAP(t, client)
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	launch := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q}`, program))
	awaitDAPSignal(t, entered)
	client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})

	response := requireCancelledLaunch(t, client, launch.Seq)
	if response.Body.Error == nil || !strings.Contains(response.Body.Error.Format, cleanupErr.Error()) {
		t.Fatalf("lost rollback detail: %#v", response)
	}

	client.read()
	client.send(&protocol.DisconnectRequest{Request: client.request("disconnect")})
	client.read()
	select {
	case err := <-client.done:
		if !errors.Is(err, cleanupErr) {
			t.Fatalf("close lost rollback identity: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not settle")
	}
}

func TestDAPPreparationShutdown(t *testing.T) {
	for _, control := range []string{"disconnect", "terminate", "EOF", "context"} {
		t.Run(control, func(t *testing.T) {
			client, entered, settled := newPreparationClient(t, "compile", nil)
			initializeDAP(t, client)
			program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
			client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q}`, program))
			awaitDAPSignal(t, entered)
			switch control {
			case "disconnect":
				client.send(&protocol.DisconnectRequest{Request: client.request("disconnect")})
				client.read()

				if _, ok := client.read().(*protocol.DisconnectResponse); !ok {
					t.Fatal("missing disconnect response")
				}
			case "terminate":
				client.send(&protocol.TerminateRequest{Request: client.request("terminate")})
				client.read()

				if response, ok := client.read().(*protocol.TerminateResponse); !ok || !response.Success {
					t.Fatalf("pending terminate = %#v", response)
				}

				client.disconnect()
			case "EOF":
				_ = client.input.Close()
			case "context":
				client.cancel()
			}

			if control != "terminate" {
				select {
				case err := <-client.done:
					if control == "context" && !errors.Is(err, context.Canceled) {
						t.Fatalf("Run = %v", err)
					}

					if control != "context" && err != nil {
						t.Fatalf("Run = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("shutdown did not settle")
				}
			}

			if settled.Load() != 1 {
				t.Fatal("late plan was not closed")
			}
		})
	}
}

func TestDAPControlsInterruptSynchronousRequests(t *testing.T) {
	for _, operation := range []string{"breakpoints", "inspection"} {
		for _, control := range []string{"cancel", "terminate", "disconnect", "EOF", "context"} {
			if operation == "inspection" && control == "cancel" {
				continue
			}

			t.Run(operation+"/"+control, func(t *testing.T) {
				native, err := uapi.New("test")
				if err != nil {
					t.Fatal(err)
				}

				entered := make(chan struct{})
				debugger := &breakpointDebugger{}
				block := func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }

				if operation == "breakpoints" {
					debugger.replaceFn = func(ctx context.Context, _ string, _ []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
						return nil, block(ctx)
					}
				} else {
					debugger.framesFn = func(ctx context.Context) ([]apidebugger.Frame, error) { return nil, block(ctx) }
				}

				client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: debugger})
				initializeDAP(t, client)
				program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
				launch := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q,"stopOnEntry":true}`, program))
				client.read()

				if operation == "inspection" {
					completePendingDAPLaunch(t, client)
					client.send(&protocol.StackTraceRequest{Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID}})
				} else {
					client.send(&protocol.SetBreakpointsRequest{Request: client.request("setBreakpoints"), Arguments: protocol.SetBreakpointsArguments{Source: protocol.Source{Path: program}, Breakpoints: []protocol.SourceBreakpoint{{Line: 1}}}})
				}

				awaitDAPSignal(t, entered)
				switch control {
				case "cancel":
					client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})
				case "terminate":
					client.send(&protocol.TerminateRequest{Request: client.request("terminate")})
				case "disconnect":
					client.send(&protocol.DisconnectRequest{Request: client.request("disconnect")})
				case "EOF":
					_ = client.input.Close()
				case "context":
					client.cancel()
				}

				if control == "EOF" || control == "context" {
					select {
					case <-client.done:
					case <-time.After(5 * time.Second):
						t.Fatal("blocked request prevented shutdown")
					}

					return
				}

				if failure, ok := client.read().(*protocol.ErrorResponse); !ok || failure.Command == "launch" {
					t.Fatalf("blocked operation response = %#v", failure)
				}

				if operation == "breakpoints" {
					response, ok := client.read().(*protocol.ErrorResponse)
					if !ok || response.Command != "launch" {
						t.Fatalf("pending response = %#v", response)
					}

					if control == "cancel" && response.Message != "cancelled" {
						t.Fatalf("launch cancellation message = %q", response.Message)
					}
				}

				if control == "cancel" {
					if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
						t.Fatal("missing cancellation termination")
					}

					if _, ok := client.read().(*protocol.CancelResponse); !ok {
						t.Fatal("missing cancel acknowledgment")
					}
				} else {
					client.read()

					if control == "terminate" {
						if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
							t.Fatal("missing terminated event")
						}
					}
				}

				if control == "disconnect" {
					waitForDAPStop(t, client)
				} else {
					client.disconnect()
				}
			})
		}
	}
}

func TestDAPBurstAndOverflow(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprint(overflow), func(t *testing.T) {
			client, entered, settled := newPreparationClient(t, "compile", nil)
			initializeDAP(t, client)
			program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
			launch := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q}`, program))
			awaitDAPSignal(t, entered)
			count := 32

			if overflow {
				count = requestQueueCapacity + 1
			}

			for range count {
				client.send(&protocol.ThreadsRequest{Request: client.request("threads")})
			}

			if overflow {
				select {
				case err := <-client.done:
					if !errors.Is(err, errRequestOverload) {
						t.Fatalf("overflow = %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("overflow waited for a DAP response")
				}

				if settled.Load() != 1 {
					t.Fatal("overflow did not close late plan")
				}

				return
			}

			client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})
			requireCancelledLaunch(t, client, launch.Seq)
			for range count {
				if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Command != "threads" {
					t.Fatalf("burst response = %#v", response)
				}
			}

			client.read()
			client.disconnect()
		})
	}
}

func TestDAPRejectedControlsAndLateCancelDoNotInterrupt(t *testing.T) {
	native, err := uapi.New("test")
	if err != nil {
		t.Fatal(err)
	}

	entered, release := make(chan struct{}), make(chan struct{})
	debugger := &breakpointDebugger{replaceFn: func(ctx context.Context, _ string, _ []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
		close(entered)
		<-release

		return nil, ctx.Err()
	}}
	client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: debugger})
	initializeDAP(t, client)
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	launch := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q,"stopOnEntry":true}`, program))
	client.read()
	client.send(&protocol.SetBreakpointsRequest{Request: client.request("setBreakpoints"), Arguments: protocol.SetBreakpointsArguments{Source: protocol.Source{Path: program}}})
	awaitDAPSignal(t, entered)
	client.send(&protocol.CancelRequest{Request: client.request("cancel")})
	client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: 999}})
	client.send(&protocol.TerminateRequest{Request: client.request("terminate"), Arguments: &protocol.TerminateArguments{Restart: true}})
	client.send(&protocol.DisconnectRequest{Request: client.request("disconnect"), Arguments: &protocol.DisconnectArguments{Restart: true}})
	client.send(&protocol.DisconnectRequest{Request: client.request("disconnect"), Arguments: &protocol.DisconnectArguments{SuspendDebuggee: true}})
	// Reading this later frame requires ingress to have processed every control.
	client.send(&protocol.ThreadsRequest{Request: client.request("threads")})
	close(release)

	if response, ok := client.read().(*protocol.SetBreakpointsResponse); !ok || !response.Success {
		t.Fatalf("rejected control cancelled breakpoint operation: %#v", response)
	}

	client.read()
	client.read()
	for range 3 {
		if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success {
			t.Fatalf("unsupported control succeeded: %#v", response)
		}
	}

	client.read()
	completePendingDAPLaunch(t, client)
	client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})

	if response, ok := client.read().(*protocol.CancelResponse); !ok || !response.Success {
		t.Fatalf("late cancel = %#v", response)
	}

	continueDAP(t, client)
	readInitialDAPCompletion(t, client)
	client.disconnect()
}

func TestDAPWatchFailureResolvesPendingLaunch(t *testing.T) {
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, writeDAPProgram(t, t.TempDir(), "RETURN 1"), "", true)
	watchErr := errors.New("test watch lost")
	client.server.failWatch(watchErr)

	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Command != "launch" || !strings.Contains(response.Message, watchErr.Error()) {
		t.Fatalf("watch failure response = %#v", response)
	}

	client.read()

	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("missing watch failure termination")
	}

	select {
	case err := <-client.done:
		if !errors.Is(err, watchErr) {
			t.Fatalf("watch failure identity = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch failure did not settle")
	}
}

func TestDAPCancelQueuedLaunchBeforeAdmission(t *testing.T) {
	client := newTestClient(t)
	initializeDAP(t, client)
	client.server.writeMu.Lock()
	// The first request blocks the sole dispatcher at its write. Ingress must
	// remember cancellation for the launch queued behind that request.
	client.send(&protocol.ThreadsRequest{Request: client.request("threads")})
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	launch := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q}`, program))
	client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})
	client.send(&protocol.ThreadsRequest{Request: client.request("threads")})
	client.server.writeMu.Unlock()
	client.read()
	requireCancelledLaunch(t, client, launch.Seq)
	client.read()
	client.read()
	sendDAPLaunch(t, client, map[string]any{"program": program, "stopOnEntry": true})
	client.read()
	completePendingDAPLaunch(t, client)
	client.disconnect()
}

func TestDAPOverflowInterruptsInFlightResponse(t *testing.T) {
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, writeDAPProgram(t, t.TempDir(), "RETURN 1"), "", true)
	completePendingDAPLaunch(t, client)
	// Leave a response unread so the writer can be blocked when overflow wins.
	// The fatal path must interrupt that write rather than depend on a peer read.
	for range requestQueueCapacity + 2 {
		request := &protocol.ThreadsRequest{Request: client.request("threads")}
		if err := protocol.WriteProtocolMessage(client.input, request); err != nil {
			break
		}
	}

	select {
	case err := <-client.done:
		if !errors.Is(err, errRequestOverload) {
			t.Fatalf("blocked-writer overflow = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("overflow cleanup depended on reading a response")
	}
}

func TestDAPTerminationInterruptsQueuedInspectionAfterLaunchSuccess(t *testing.T) {
	native, err := uapi.New("test")
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	var calls atomic.Int32
	debugger := &breakpointDebugger{framesFn: func(ctx context.Context) ([]apidebugger.Frame, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}

		<-ctx.Done()

		return nil, ctx.Err()
	}}
	client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: debugger})
	initializeDAP(t, client)
	launchDAP(t, client, writeDAPProgram(t, t.TempDir(), "RETURN 1"), "", true)
	completePendingDAPLaunch(t, client)
	inspection := func() {
		client.send(&protocol.StackTraceRequest{Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID}})
	}
	inspection()
	awaitDAPSignal(t, entered)
	inspection()
	client.send(&protocol.TerminateRequest{Request: client.request("terminate")})
	for range 2 {
		if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Command != "stackTrace" {
			t.Fatalf("inspection after termination intent = %#v", response)
		}
	}

	if _, ok := client.read().(*protocol.TerminateResponse); !ok {
		t.Fatal("missing terminate response")
	}

	if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
		t.Fatal("missing terminal event")
	}

	if calls.Load() != 1 {
		t.Fatal("queued inspection entered the runtime after termination intent")
	}

	client.disconnect()
}
