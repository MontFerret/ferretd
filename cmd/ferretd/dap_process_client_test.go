package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"

	protocol "github.com/google/go-dap"
)

type (
	dapProcessClient struct {
		t        *testing.T
		command  *exec.Cmd
		input    io.WriteCloser
		output   *bufio.Reader
		stderr   bytes.Buffer
		sequence int
		waited   bool
	}

	dapProcessMessage struct {
		Type    string          `json:"type"`
		Command string          `json:"command"`
		Event   string          `json:"event"`
		Success bool            `json:"success"`
		Body    json.RawMessage `json:"body"`
	}
)

func newDAPProcessClient(t *testing.T, binary string) *dapProcessClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	client := &dapProcessClient{t: t, command: exec.CommandContext(ctx, binary, "dap")}
	client.command.Stderr = &client.stderr

	input, err := client.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	output, err := client.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	client.input = input

	client.output = bufio.NewReader(output)
	if err := client.command.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = client.input.Close()
		if !client.waited {
			_ = client.command.Process.Kill()
			_ = client.command.Wait()
		}
	})

	return client
}

func (c *dapProcessClient) send(command string, arguments any) {
	c.t.Helper()
	c.sequence++

	content, err := json.Marshal(map[string]any{
		"seq": c.sequence, "type": "request", "command": command, "arguments": arguments,
	})
	if err != nil {
		c.t.Fatal(err)
	}

	if err := protocol.WriteBaseMessage(c.input, content); err != nil {
		c.t.Fatal(err)
	}
}

func (c *dapProcessClient) read() dapProcessMessage {
	c.t.Helper()
	// The command context kills the process and closes stdout if it stops
	// responding. Keep protocol reads synchronous so no test reader outlives it.
	content, err := protocol.ReadBaseMessage(c.output)
	if err != nil {
		c.t.Fatalf("read DAP process: %v", err)
	}

	var message dapProcessMessage
	if err := json.Unmarshal(content, &message); err != nil {
		c.t.Fatal(err)
	}

	return message
}

func (c *dapProcessClient) response(command string) dapProcessMessage {
	c.t.Helper()

	message := c.read()
	if message.Type != "response" || message.Command != command || !message.Success {
		c.t.Fatalf("expected successful %s response, got %+v", command, message)
	}

	return message
}

func (c *dapProcessClient) event(event string) dapProcessMessage {
	c.t.Helper()

	message := c.read()
	if message.Type != "event" || message.Event != event {
		c.t.Fatalf("expected %s event, got %+v", event, message)
	}

	return message
}

func (c *dapProcessClient) disconnect() {
	c.t.Helper()
	c.send("disconnect", map[string]any{})
	c.response("disconnect")
	_ = c.input.Close()
	err := c.command.Wait()
	c.waited = true

	if err != nil {
		c.t.Fatalf("DAP process exit: %v; stderr: %s", err, c.stderr.String())
	}
}
