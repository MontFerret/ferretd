package dap

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2/uapi"
)

func TestDAPShutdownCancelsEarlierLaunches(t *testing.T) {
	for _, control := range []string{"terminate", "disconnect"} {
		t.Run(control, func(t *testing.T) {
			native, err := uapi.New("test")
			if err != nil {
				t.Fatal(err)
			}

			entered := make(chan struct{}, 1)
			var compiled, closed atomic.Int32
			runtime := &preparationRuntime{Runtime: native}
			runtime.compileFn = func(ctx context.Context, source api.Source, _ bool) (api.Plan, error) {
				compiled.Add(1)
				entered <- struct{}{}
				<-ctx.Done()
				plan, compileErr := native.Compile(context.Background(), source)

				return &preparationPlan{Plan: plan, closeFn: func() error { closed.Add(1); return nil }}, compileErr
			}
			client := newTestClientWithRuntime(t, Options{}, runtime)
			initializeDAP(t, client)
			program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
			arguments := fmt.Sprintf(`{"program":%q}`, program)
			first := client.sendRawRequest("launch", arguments)
			awaitDAPSignal(t, entered)
			second := client.sendRawRequest("launch", arguments)
			shutdown := client.sendRawRequest(control, `{}`)

			for _, sequence := range []int{first.Seq, second.Seq} {
				failure, ok := client.read().(*protocol.ErrorResponse)
				if !ok || failure.Command != "launch" || failure.RequestSeq != sequence {
					t.Fatalf("launch %d failure = %#v", sequence, failure)
				}
			}

			response, ok := client.read().(protocol.ResponseMessage)
			if !ok || !response.GetResponse().Success || response.GetResponse().RequestSeq != shutdown.Seq {
				t.Fatalf("%s acknowledgment = %#v", control, response)
			}

			if compiled.Load() != 1 || closed.Load() != 1 {
				t.Fatalf("preparation/rollback calls = %d/%d, want 1/1", compiled.Load(), closed.Load())
			}

			workspaces, err := client.server.workspaces.List(t.Context())
			if err != nil || len(workspaces) != 0 {
				t.Fatalf("shutdown retained workspaces: %v, %v", workspaces, err)
			}

			if control == "disconnect" {
				waitForDAPStop(t, client)

				return
			}

			// No published launch means no terminated event; the adapter remains
			// available for the disconnect acknowledgment without extra frames.
			client.disconnect()
		})
	}
}

func TestDAPShutdownBeforeLaunchDispatch(t *testing.T) {
	for _, control := range []string{"terminate", "disconnect"} {
		for _, initialization := range []string{"before launches", "between launches", "absent"} {
			t.Run(control+"/initialize "+initialization, func(t *testing.T) {
				native, err := uapi.New("test")
				if err != nil {
					t.Fatal(err)
				}

				compiled := 0
				runtime := &preparationRuntime{Runtime: native, compileFn: func(context.Context, api.Source, bool) (api.Plan, error) {
					compiled++

					return nil, errors.New("unexpected runtime preparation")
				}}
				var output bytes.Buffer

				server, err := newServer(strings.NewReader(""), &output, Options{}.normalized(), runtime, mustNewRuntimeCatalog(t))
				if err != nil {
					t.Fatal(err)
				}

				t.Cleanup(func() {
					if err := server.cleanup(); err != nil {
						t.Error(err)
					}
				})
				client := &testClient{t: t}
				program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
				arguments := []byte(fmt.Sprintf(`{"program":%q}`, program))
				initialize := &protocol.InitializeRequest{Request: client.request("initialize")}
				first := &protocol.LaunchRequest{Request: client.request("launch"), Arguments: arguments}
				second := &protocol.LaunchRequest{Request: client.request("launch"), Arguments: arguments}
				requests := []protocol.RequestMessage{initialize, first, second}

				switch initialization {
				case "between launches":
					// The first launch is rejected before admission. Termination
					// must still recognize the later admitted attempt.
					requests = []protocol.RequestMessage{first, initialize, second}
					first.Seq, initialize.Seq = initialize.Seq, first.Seq
				case "absent":
					requests = []protocol.RequestMessage{first, second}
				}

				shutdown := client.request(control)
				if control == "terminate" {
					requests = append(requests, &protocol.TerminateRequest{Request: shutdown})
				} else {
					requests = append(requests, &protocol.DisconnectRequest{Request: shutdown})
				}

				queue := make(chan requestEnvelope, len(requests))
				for _, request := range requests {
					queue <- server.receiveRequest(request, initializeClientOptions{})
				}

				close(queue)

				if err := server.serveRequests(t.Context(), queue); err != nil {
					t.Fatal(err)
				}

				if compiled != 0 {
					t.Fatalf("queued shutdown allowed %d runtime preparations", compiled)
				}

				reader := bufio.NewReader(&output)
				for _, request := range requests {
					message, err := protocol.ReadProtocolMessage(reader)
					if err != nil {
						t.Fatal(err)
					}

					wantSuccess := request.GetRequest().Command != "launch"

					if request.GetRequest().Command == "terminate" && initialization == "absent" {
						wantSuccess = false
					}

					response, ok := message.(protocol.ResponseMessage)
					if !ok || response.GetResponse().RequestSeq != request.GetSeq() || response.GetResponse().Success != wantSuccess {
						t.Fatalf("response to %s = %#v", request.GetRequest().Command, message)
					}
				}

				if message, err := protocol.ReadProtocolMessage(reader); !errors.Is(err, io.EOF) {
					t.Fatalf("unexpected extra frame: %#v, %v", message, err)
				}
			})
		}
	}
}

func TestDAPTerminatePreparationAllowsLaterLaunch(t *testing.T) {
	client, entered, settled := newPreparationClient(t, "compile", nil)
	initializeDAP(t, client)
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	first := client.sendRawRequest("launch", fmt.Sprintf(`{"program":%q}`, program))
	awaitDAPSignal(t, entered)
	client.sendRawRequest("terminate", `{}`)

	if failure, ok := client.read().(*protocol.ErrorResponse); !ok || failure.RequestSeq != first.Seq {
		t.Fatalf("terminated launch = %#v", failure)
	}

	if response, ok := client.read().(*protocol.TerminateResponse); !ok || !response.Success {
		t.Fatalf("terminate acknowledgment = %#v", response)
	}

	sendDAPLaunch(t, client, map[string]any{"program": program, "stopOnEntry": true})

	if _, ok := client.read().(*protocol.InitializedEvent); !ok {
		t.Fatal("termination before publication prevented retry")
	}

	completePendingDAPLaunch(t, client)
	client.disconnect()

	if settled.Load() != 1 {
		t.Fatalf("terminated preparation settlement count = %d, want 1", settled.Load())
	}
}

func TestDAPCancelLeavesOtherQueuedLaunchUnchanged(t *testing.T) {
	client, entered, settled := newPreparationClient(t, "compile", nil)
	initializeDAP(t, client)
	program := writeDAPProgram(t, t.TempDir(), "RETURN 1")
	arguments := fmt.Sprintf(`{"program":%q,"stopOnEntry":true}`, program)
	first := client.sendRawRequest("launch", arguments)
	awaitDAPSignal(t, entered)
	client.sendRawRequest("launch", arguments)
	client.send(&protocol.CancelRequest{Request: client.request("cancel"), Arguments: &protocol.CancelArguments{RequestId: first.Seq}})
	requireCancelledLaunch(t, client, first.Seq)

	if _, ok := client.read().(*protocol.InitializedEvent); !ok {
		t.Fatal("targeted cancellation prevented the other launch")
	}

	if response, ok := client.read().(*protocol.CancelResponse); !ok || !response.Success {
		t.Fatalf("cancel acknowledgment = %#v", response)
	}

	completePendingDAPLaunch(t, client)
	client.disconnect()

	if settled.Load() != 1 {
		t.Fatalf("cancelled preparation settlement count = %d, want 1", settled.Load())
	}
}
