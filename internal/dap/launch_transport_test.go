package dap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	apidebugger "github.com/MontFerret/api/debugger"
	"github.com/MontFerret/ferret/v2/uapi"
	"github.com/MontFerret/ferretd/internal/debug"
	"github.com/MontFerret/ferretd/internal/exec"
)

func TestDAPInitializedWriteFailureSettlesPublishedResources(t *testing.T) {
	client := newTestClient(t)
	initializeDAP(t, client)
	writeErr := errors.New("initialized transport lost")
	client.server.writeMu.Lock()
	client.server.writer = failedDAPWriter{cause: writeErr}
	client.server.writeMu.Unlock()
	sendDAPLaunch(t, client, map[string]any{"program": writeDAPProgram(t, t.TempDir(), "RETURN 1")})
	select {
	case err := <-client.done:
		if !errors.Is(err, writeErr) {
			t.Fatalf("transport failure = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initialized failure did not settle")
	}

	server := client.server
	if _, err := server.debugs.GetSession(t.Context(), server.owned.debug); !errors.Is(err, debug.ErrSessionNotFound) {
		t.Fatalf("debugger retained after write failure: %v", err)
	}

	if _, err := server.executions.GetSession(t.Context(), server.owned.session); !errors.Is(err, exec.ErrSessionNotFound) {
		t.Fatalf("compiled Session retained after write failure: %v", err)
	}
}

func TestDAPWatchFailureInterruptDoesNotWaitForEventLock(t *testing.T) {
	native, err := uapi.New("test")
	if err != nil {
		t.Fatal(err)
	}

	entered, interrupted, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	debugger := &breakpointDebugger{replaceFn: func(ctx context.Context, _ string, _ []apidebugger.BreakpointRequest) ([]apidebugger.Breakpoint, error) {
		close(entered)
		<-ctx.Done()
		close(interrupted)
		<-release

		return nil, ctx.Err()
	}}
	client := newTestClientWithRuntime(t, Options{}, &breakpointRuntime{Runtime: native, debugger: debugger})
	initializeDAP(t, client)
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	launchDAP(t, client, program, "", true)
	client.send(&protocol.SetBreakpointsRequest{Request: client.request("setBreakpoints"), Arguments: protocol.SetBreakpointsArguments{Source: protocol.Source{Path: program}}})
	awaitDAPSignal(t, entered)
	signalled := make(chan struct{})
	watchErr := errors.New("watch failed behind event lock")
	go func() { client.server.failWatch(watchErr); close(signalled) }()
	awaitDAPSignal(t, signalled)
	awaitDAPSignal(t, interrupted)
	close(release)
	client.read()
	client.read()
	client.read()
	client.read()
	select {
	case err := <-client.done:
		if !errors.Is(err, watchErr) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch failure did not end adapter")
	}
}

func TestDAPCancelDuringConfigurationEndsOneLaunch(t *testing.T) {
	client := newTestClient(t)
	initializeDAP(t, client)
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	launch := client.sendRawRequest("launch", `{"program":`+quotedDAPString(t, program)+`}`)
	client.read()
	client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: launch.Seq}})
	requireCancelledLaunch(t, client, launch.Seq)
	client.read()
	client.read()
	sendDAPLaunch(t, client, map[string]any{"program": program})

	if response, ok := client.read().(*protocol.ErrorResponse); !ok || response.Success {
		t.Fatalf("relaunch after initialized succeeded: %#v", response)
	}

	client.disconnect()
}

func TestDAPControlsDuringConfigurationResponse(t *testing.T) {
	for _, control := range []string{"cancel", "terminate", "disconnect"} {
		t.Run(control, func(t *testing.T) {
			client := newTestClient(t)
			initializeDAP(t, client)
			program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
			launch := client.sendRawRequest("launch", `{"program":`+quotedDAPString(t, program)+`,"stopOnEntry":true}`)
			client.read()
			writer := client.gateWrites()
			configuration := client.request("configurationDone")
			client.send(&protocol.ConfigurationDoneRequest{Request: configuration})
			awaitDAPSignal(t, writer.entered)

			// Observe actual ingress cancellation before allowing StartSession.
			// Forward to the original callback so the operation context is real.
			interrupted := make(chan struct{})
			var once sync.Once
			client.server.stateMu.Lock()
			cancel := client.server.activeCancel
			client.server.activeCancel = func(cause error) {
				cancel(cause)
				once.Do(func() { close(interrupted) })
			}
			client.server.stateMu.Unlock()
			arguments := `{}`

			if control == "cancel" {
				arguments = fmt.Sprintf(`{"requestId":%d}`, launch.Seq)
			}

			shutdown := client.sendRawRequest(control, arguments)
			awaitDAPSignal(t, interrupted)
			writer.unblock()

			if response, ok := client.read().(*protocol.ConfigurationDoneResponse); !ok || !response.Success || response.RequestSeq != configuration.Seq {
				t.Fatalf("configuration response = %#v", response)
			}

			failure, ok := client.read().(*protocol.ErrorResponse)
			if !ok || failure.Command != "launch" || failure.RequestSeq != launch.Seq || failure.Body.Error == nil {
				t.Fatalf("launch failure = %#v", failure)
			}

			if !strings.Contains(failure.Body.Error.Format, context.Canceled.Error()) {
				t.Fatalf("launch failure lost start error: %#v", failure)
			}

			if control == "cancel" {
				if failure.Message != "cancelled" {
					t.Fatalf("cancelled launch message = %q", failure.Message)
				}

				if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
					t.Fatal("missing cancellation termination")
				}
			}

			response, ok := client.read().(protocol.ResponseMessage)
			if !ok || !response.GetResponse().Success || response.GetResponse().RequestSeq != shutdown.Seq {
				t.Fatalf("%s acknowledgment = %#v", control, response)
			}

			if control == "terminate" {
				if _, ok := client.read().(*protocol.TerminatedEvent); !ok {
					t.Fatal("missing termination event")
				}
			}

			if control == "disconnect" {
				waitForDAPStop(t, client)

				if message, err := protocol.ReadProtocolMessage(client.output); !errors.Is(err, io.EOF) {
					t.Fatalf("expected EOF after disconnect, got %#v, %v", message, err)
				}

				return
			}

			client.disconnect()
		})
	}
}

func TestDAPTerminationPreservesRecognizedStaleHandles(t *testing.T) {
	client := newTestClient(t)
	initializeDAP(t, client)
	launchDAP(t, client, writeDAPProgram(t, t.TempDir(), "RETURN 1"), "", true)
	completePendingDAPLaunch(t, client)
	client.send(&protocol.StackTraceRequest{Request: client.request("stackTrace"), Arguments: protocol.StackTraceArguments{ThreadId: threadID}})
	stack := client.read().(*protocol.StackTraceResponse)
	client.send(&protocol.TerminateRequest{Request: client.request("terminate")})
	client.read()
	client.read()
	client.send(&protocol.ScopesRequest{Request: client.request("scopes"), Arguments: protocol.ScopesArguments{FrameId: stack.Body.StackFrames[0].Id}})

	if response, ok := client.read().(*protocol.ScopesResponse); !ok || !response.Success || len(response.Body.Scopes) != 0 {
		t.Fatalf("late scopes = %#v", response)
	}

	client.disconnect()
}

func quotedDAPString(t *testing.T, value string) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}

	return string(encoded)
}
