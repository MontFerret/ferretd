package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/google/go-dap"
)

func BenchmarkDAPNativeLaunchCleanup(b *testing.B) {
	root := b.TempDir()

	program := filepath.Join(root, "query.fql")
	if err := os.WriteFile(program, []byte("RETURN 1"), 0o600); err != nil {
		b.Fatal(err)
	}

	arguments, err := json.Marshal(map[string]any{"program": program, "cwd": root, "stopOnEntry": true})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		benchmarkDAPLaunch(b, arguments)
	}
}

func benchmarkDAPLaunch(b *testing.B, arguments json.RawMessage) {
	b.Helper()
	input, requests := io.Pipe()
	responses, output := io.Pipe()
	defer func() {
		_ = requests.Close()
		_ = responses.Close()
	}()

	server, err := New(input, output, Options{})
	if err != nil {
		b.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- server.Run(context.Background()) }()
	reader := bufio.NewReader(responses)
	sequence := 0
	request := func(command string) protocol.Request {
		sequence++

		return protocol.Request{ProtocolMessage: protocol.ProtocolMessage{Seq: sequence, Type: "request"}, Command: command}
	}
	send := func(message protocol.Message) {
		if err := protocol.WriteProtocolMessage(requests, message); err != nil {
			b.Fatal(err)
		}
	}
	read := func() {
		message, err := protocol.ReadProtocolMessage(reader)
		if err != nil {
			b.Fatal(err)
		}

		if response, ok := message.(protocol.ResponseMessage); ok && !response.GetResponse().Success {
			b.Fatal(response.GetResponse().Message)
		}
	}
	send(&protocol.InitializeRequest{Request: request("initialize")})
	read()
	send(&protocol.LaunchRequest{Request: request("launch"), Arguments: arguments})
	read()
	send(&protocol.ConfigurationDoneRequest{Request: request("configurationDone")})
	read()
	read()
	read()
	send(&protocol.TerminateRequest{Request: request("terminate")})
	read()
	read()
	send(&protocol.DisconnectRequest{Request: request("disconnect")})
	read()

	if err := <-done; err != nil {
		b.Fatal(err)
	}
}
