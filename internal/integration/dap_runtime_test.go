package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/google/go-dap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	wireclient "github.com/MontFerret/wire/client"
	wirev1 "github.com/MontFerret/wire/gen/ferret/wire/v1"
	wireserver "github.com/MontFerret/wire/server"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

func TestDAPWireRuntimeDebuggingAndHostOwnership(t *testing.T) {
	for _, ending := range []string{"complete", "terminate", "disconnect"} {
		t.Run(ending, func(t *testing.T) {
			root := t.TempDir()
			program := filepath.Join(root, "query.fql")
			const text = "LET remote = TASK3_APPLICATION_VALUE()\nLET values = [remote, @offset]\nLET total = remote + @offset\nRETURN total"
			writeSourceFile(t, root, "query.fql", text)
			var normalCompiles, debugCompiles, closedPlans, releasedDebug atomic.Int32
			var firstConnection atomic.Bool
			bindingEnded := make(chan struct{})
			roots := make(chan *string, 8)
			retainedSource := make(chan string, 1)
			wire := newWireFixture(t, 41, wireserver.WithUnaryInterceptors(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
				switch value := request.(type) {
				case *wirev1.CompileRequest:
					normalCompiles.Add(1)
					result, err := next(ctx, request)
					// Lazy debug compilation must still use the original bytes.
					if err == nil {
						err = os.WriteFile(program, []byte("RETURN 999"), 0o600)
					}

					return result, err
				case *wirev1.CompileDebugRequest:
					debugCompiles.Add(1)
					retainedSource <- value.Source.Content
				case *wirev1.CreateDebugSessionRequest:
					roots <- value.FsRoot
				case *wirev1.ClosePlanRequest:
					closedPlans.Add(1)
				case *wirev1.ReleaseDebugSessionRequest:
					releasedDebug.Add(1)
				}

				return next(ctx, request)
			}), wireserver.WithStreamInterceptors(func(host any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
				if strings.HasSuffix(info.FullMethod, "/Connect") && firstConnection.CompareAndSwap(false, true) {
					defer close(bindingEnded)
				}

				return next(host, stream)
			}))
			configuration := runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}}

			if ending == "complete" {
				configuration.DefaultRuntime = new("application")
			}

			client := newFramedDAPClient(t, configuration)
			client.send("initialize", map[string]any{})
			initialized := client.response("initialize", true)

			var capabilities map[string]bool
			if err := json.Unmarshal(initialized.Body, &capabilities); err != nil || !capabilities["supportsFerretRuntimeSelection"] || !capabilities["supportsCancelRequest"] {
				t.Fatalf("initialize capabilities = %s, %v", initialized.Body, err)
			}

			if wire.acquisitions.Load() != 0 {
				t.Fatal("initialize dialed the Wire default")
			}

			arguments := map[string]any{"program": program, "cwd": root, "parameters": map[string]any{"offset": 1}, "clientSpecific": true}

			if ending != "complete" {
				arguments["runtimeName"] = "application"
			}

			client.send("launch", arguments)
			client.event("initialized")

			if fsRoot := <-roots; fsRoot != nil {
				t.Fatalf("DAP sent implicit local FS root: %q", *fsRoot)
			}

			if source := <-retainedSource; source != text {
				t.Fatalf("debug source changed: %q", source)
			}

			client.send("setBreakpoints", map[string]any{"source": map[string]any{"path": "query.fql"}, "breakpoints": []map[string]int{{"line": 2}}})
			client.response("setBreakpoints", true)
			client.send("configurationDone", map[string]any{})
			client.response("configurationDone", true)
			launch := client.response("launch", true)

			var acknowledgment struct {
				RuntimeName string `json:"runtimeName"`
			}
			if err := json.Unmarshal(launch.Body, &acknowledgment); err != nil || acknowledgment.RuntimeName != "application" {
				t.Fatalf("runtime acknowledgment = %s, %v", launch.Body, err)
			}

			stopped := client.event("stopped")
			if !strings.Contains(string(stopped.Body), `"reason":"breakpoint"`) {
				t.Fatalf("breakpoint stop = %s", stopped.Body)
			}

			if wire.acquisitions.Load() != 1 || normalCompiles.Load() != 1 || debugCompiles.Load() != 1 {
				t.Fatalf("DAP acquired/compiled more than once: %d/%d/%d", wire.acquisitions.Load(), normalCompiles.Load(), debugCompiles.Load())
			}

			client.send("stackTrace", map[string]any{"threadId": 1})
			framesMessage := client.response("stackTrace", true)

			var frames protocol.StackTraceResponseBody
			if err := json.Unmarshal(framesMessage.Body, &frames); err != nil || len(frames.StackFrames) != 1 {
				t.Fatalf("frames = %s, %v", framesMessage.Body, err)
			}

			frame := frames.StackFrames[0]
			if frame.Source == nil || frame.Source.Path != program || frame.Line != 2 {
				t.Fatalf("local immutable source identity = %+v", frame)
			}

			client.send("scopes", map[string]any{"frameId": frame.Id})
			scopesMessage := client.response("scopes", true)

			var scopes protocol.ScopesResponseBody
			if err := json.Unmarshal(scopesMessage.Body, &scopes); err != nil || len(scopes.Scopes) != 2 {
				t.Fatalf("scopes = %s, %v", scopesMessage.Body, err)
			}

			foundRemote := false
			for _, scope := range scopes.Scopes {
				client.send("variables", map[string]any{"variablesReference": scope.VariablesReference})
				variablesMessage := client.response("variables", true)

				var variables protocol.VariablesResponseBody
				if err := json.Unmarshal(variablesMessage.Body, &variables); err != nil {
					t.Fatal(err)
				}

				for _, value := range variables.Variables {
					if value.Name == "remote" && value.Value == "41" {
						foundRemote = true
					}
				}
			}

			if !foundRemote {
				t.Fatal("host-specific local value missing")
			}

			client.send("evaluate", map[string]any{"frameId": frame.Id, "expression": "@offset", "context": "repl"})

			evaluation := client.response("evaluate", true)
			if !strings.Contains(string(evaluation.Body), `"result":"1"`) {
				t.Fatalf("evaluation = %s", evaluation.Body)
			}

			switch ending {
			case "complete":
				client.send("next", map[string]any{"threadId": 1})
				client.response("next", true)
				client.event("stopped")
				client.send("continue", map[string]any{"threadId": 1})
				client.response("continue", true)

				output := client.event("output")
				if !strings.Contains(string(output.Body), `42\n`) {
					t.Fatalf("host output = %s", output.Body)
				}

				client.event("exited")
				client.event("terminated")
			case "terminate":
				client.send("terminate", map[string]any{})
				client.response("terminate", true)
				client.event("terminated")

				if closedPlans.Load() != 2 || releasedDebug.Load() != 1 {
					t.Fatalf("terminate retained query resources: plans=%d debugger=%d", closedPlans.Load(), releasedDebug.Load())
				}

				select {
				case <-bindingEnded:
				case <-time.After(5 * time.Second):
					t.Fatal("terminate retained the runtime binding")
				}

				assertFreshDAPHost(t, wire)
			}

			client.disconnect()
			select {
			case <-bindingEnded:
			case <-time.After(5 * time.Second):
				t.Fatal("disconnect retained runtime binding")
			}

			if wire.closes.Load() != 0 {
				t.Fatal("DAP closed its borrowed application runtime")
			}

			assertFreshDAPHost(t, wire)
		})
	}
}

func TestDAPWireWorkingDirectoryIsHostValidated(t *testing.T) {
	const hostDirectory = `  X:\application\data  `
	received := make(chan string, 1)
	wire := newWireFixture(t, 41, wireserver.WithUnaryInterceptors(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if create, ok := request.(*wirev1.CreateDebugSessionRequest); ok {
			received <- create.GetFsRoot()

			return nil, status.Error(codes.InvalidArgument, "application rejected its working directory")
		}

		return next(ctx, request)
	}))
	client := newFramedDAPClient(t, runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})
	client.send("initialize", map[string]any{})
	client.response("initialize", true)

	program := filepath.Join(t.TempDir(), "query.fql")
	if err := os.WriteFile(program, []byte("RETURN TASK3_APPLICATION_VALUE()"), 0o600); err != nil {
		t.Fatal(err)
	}

	client.send("launch", map[string]any{"program": program, "runtimeName": "application", "workingDirectory": hostDirectory})

	failure := client.response("launch", false)
	if !strings.Contains(failure.Message, "application") || !strings.Contains(failure.Message, "application rejected its working directory") {
		t.Fatalf("host error context = %s", failure.Message)
	}

	if directory := <-received; directory != hostDirectory {
		t.Fatalf("host directory was normalized: %q", directory)
	}

	client.disconnect()
}

func TestDAPWireAcquisitionCancellationAndArgumentValidation(t *testing.T) {
	entered := make(chan struct{})
	var connections atomic.Int32
	wire := newWireFixture(t, 41, wireserver.WithStreamInterceptors(func(host any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		if strings.HasSuffix(info.FullMethod, "/Connect") && connections.Add(1) == 1 {
			close(entered)
			<-stream.Context().Done()

			return stream.Context().Err()
		}

		return next(host, stream)
	}))
	client := newFramedDAPClient(t, runtimes.Config{DefaultRuntime: new("application"), Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})
	client.send("initialize", map[string]any{})
	client.response("initialize", true)

	program := filepath.Join(t.TempDir(), "query.fql")
	if err := os.WriteFile(program, []byte("RETURN 1"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, field := range []map[string]any{{"noDebug": true}, {"noDebug": nil}, {"runtimeName": nil}, {"runtimeName": "unknown"}} {
		field["program"] = program
		client.send("launch", field)
		client.response("launch", false)
	}

	if connections.Load() != 0 {
		t.Fatal("argument rejection acquired a Wire runtime")
	}

	launch := client.send("launch", map[string]any{"program": program, "runtimeName": ""})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("launch did not acquire the default")
	}

	client.send("cancel", map[string]any{"requestId": launch})

	failure := client.response("launch", false)
	if failure.Message != "cancelled" {
		t.Fatalf("acquisition cancel = %+v", failure)
	}

	client.response("cancel", true)
	client.send("launch", map[string]any{"program": program, "runtimeName": "local", "stopOnEntry": true})
	client.event("initialized")
	client.send("configurationDone", map[string]any{})
	client.response("configurationDone", true)

	acknowledgment := client.response("launch", true)
	if !strings.Contains(string(acknowledgment.Body), `"runtimeName":"local"`) {
		t.Fatalf("retry selection = %s", acknowledgment.Body)
	}

	client.event("stopped")
	client.disconnect()

	fresh, err := wireclient.New(t.Context(), strings.TrimPrefix(wire.endpoint, "tcp://"), wireclient.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}

	assertApplicationResult(t, fresh, 42)

	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDAPWirePreparationFailuresRemainRetryable(t *testing.T) {
	for _, stage := range []string{"compile", "debug compile", "debugger"} {
		t.Run(stage, func(t *testing.T) {
			wire := newWireFixture(t, 41, wireserver.WithUnaryInterceptors(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
				fail := false
				switch request.(type) {
				case *wirev1.CompileRequest:
					fail = stage == "compile"
				case *wirev1.CompileDebugRequest:
					fail = stage == "debug compile"
				case *wirev1.CreateDebugSessionRequest:
					fail = stage == "debugger"
				}

				if fail {
					return nil, status.Error(codes.Internal, "application "+stage+" failed")
				}

				return next(ctx, request)
			}))
			client := newFramedDAPClient(t, runtimes.Config{Runtimes: []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: wire.endpoint, Insecure: true}}})

			program := filepath.Join(t.TempDir(), "query.fql")
			if err := os.WriteFile(program, []byte("RETURN 1"), 0o600); err != nil {
				t.Fatal(err)
			}

			client.send("initialize", map[string]any{})
			client.response("initialize", true)
			client.send("launch", map[string]any{"program": program, "runtimeName": "application"})

			failure := client.response("launch", false)
			if !strings.Contains(failure.Message, "application "+stage+" failed") {
				t.Fatalf("preparation lost nondiagnostic cause: %+v", failure)
			}

			client.send("launch", map[string]any{"program": program, "runtimeName": "local", "stopOnEntry": true})
			client.event("initialized")
			client.send("configurationDone", map[string]any{})
			client.response("configurationDone", true)
			client.response("launch", true)
			client.event("stopped")
			client.disconnect()

			if wire.closes.Load() != 0 {
				t.Fatal("preparation rollback closed application")
			}
		})
	}
}

// A fresh connection and actual compile/run prove host liveness independently of
// cached runtime metadata or any connection retained by DAP.
func assertFreshDAPHost(t *testing.T, wire *wireFixture) {
	t.Helper()

	fresh, err := wireclient.New(t.Context(), strings.TrimPrefix(wire.endpoint, "tcp://"), wireclient.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}

	assertApplicationResult(t, fresh, 42)

	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
}
