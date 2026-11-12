package dap

import (
	"encoding/json"
	"strings"
	"testing"

	protocol "github.com/google/go-dap"

	"github.com/MontFerret/ferretd/internal/runtimes"
)

func TestDAPRawRuntimeCapabilitiesAndAcknowledgment(t *testing.T) {
	for _, selector := range []string{"", `,"runtimeName":""`, `,"runtimeName":"local"`} {
		t.Run(selector, func(t *testing.T) {
			client := newTestClient(t)
			client.send(&protocol.InitializeRequest{Request: client.request("initialize")})
			initialized := client.readRaw()

			var capabilities map[string]bool
			if err := json.Unmarshal(initialized["body"], &capabilities); err != nil {
				t.Fatal(err)
			}

			for _, name := range []string{"supportsFerretRuntimeSelection", "supportsCancelRequest", "supportsConfigurationDoneRequest", "supportsEvaluateForHovers", "supportsTerminateRequest"} {
				if !capabilities[name] {
					t.Fatalf("missing capability %q: %s", name, initialized["body"])
				}
			}

			root := t.TempDir()
			program := writeDAPProgram(t, root, "RETURN 1")
			encoded, _ := json.Marshal(program)
			client.sendRawRequest("launch", `{"program":`+string(encoded)+`,"stopOnEntry":true,"clientMetadata":{} `+selector+`}`)

			if _, ok := client.read().(*protocol.InitializedEvent); !ok {
				t.Fatal("missing initialized event")
			}

			client.send(&protocol.ConfigurationDoneRequest{Request: client.request("configurationDone")})
			client.read()
			launch := client.readRaw()

			var body launchResponseBody
			if err := json.Unmarshal(launch["body"], &body); err != nil || body.RuntimeName != "local" || string(launch["success"]) != "true" {
				t.Fatalf("launch JSON = %v, %v", launch, err)
			}

			client.read()
			client.disconnect()
		})
	}
}

func TestDAPSelectionValidationBeforePreparation(t *testing.T) {
	cases := []string{
		`"runtimeName":null`, `"runtimeName":4`, `"runtimeName":true`, `"runtimeName":[]`, `"runtimeName":{}`,
		`"noDebug":null`, `"noDebug":1`, `"noDebug":"false"`, `"noDebug":true`,
		`"runtimeName":" local "`, `"runtimeName":"unknown"`, `"runtimeName":"tcp://localhost:9000"`,
	}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			client := newTestClient(t)
			initializeDAP(t, client)
			program, _ := json.Marshal(writeDAPProgram(t, t.TempDir(), "RETURN 1"))
			client.sendRawRequest("launch", `{"program":`+string(program)+`,`+field+`}`)

			response, ok := client.read().(*protocol.ErrorResponse)
			if !ok || response.Success || response.Command != "launch" {
				t.Fatalf("invalid launch = %#v", response)
			}

			workspaces, err := client.server.workspaces.List(t.Context())
			if err != nil || len(workspaces) != 0 {
				t.Fatalf("failed launch retained workspaces: %v, %v", workspaces, err)
			}

			sendDAPLaunch(t, client, map[string]any{"program": stringMustDecode(t, program), "noDebug": false, "stopOnEntry": true})
			client.read()
			completePendingDAPLaunch(t, client)
			client.disconnect()
		})
	}
}

func stringMustDecode(t *testing.T, value json.RawMessage) string {
	t.Helper()

	var result string
	if err := json.Unmarshal(value, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func TestDAPUnavailableWireDefaultIsLazy(t *testing.T) {
	client := newTestClientWithOptions(t, Options{RuntimeConfig: runtimes.Config{
		DefaultRuntime: new("application"),
		Runtimes:       []runtimes.DefinitionConfig{{Name: "application", Provider: runtimes.ProviderWire, Endpoint: "tcp://127.0.0.1:1", Insecure: true, ConnectTimeout: new("20ms")}},
	}})
	initializeDAP(t, client)
	sendDAPLaunch(t, client, map[string]any{"program": writeDAPProgram(t, t.TempDir(), "RETURN 1")})

	failure, ok := client.read().(*protocol.ErrorResponse)
	if !ok || !strings.Contains(failure.Message, "application") || !strings.Contains(failure.Message, "runtime unavailable") {
		t.Fatalf("unavailable default = %#v", failure)
	}

	sendDAPLaunch(t, client, map[string]any{"program": writeDAPProgram(t, t.TempDir(), "RETURN 2"), "runtimeName": "local", "stopOnEntry": true})
	client.read()
	completePendingDAPLaunch(t, client)
	client.disconnect()
}
