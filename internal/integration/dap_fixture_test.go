package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	protocol "github.com/google/go-dap"

	ferretdap "github.com/MontFerret/ferretd/internal/dap"
	"github.com/MontFerret/ferretd/internal/runtimes"
)

type (
	framedDAPClient struct {
		t        *testing.T
		requests *io.PipeWriter
		messages chan framedDAPMessage
		stopped  chan struct{}
		readDone chan struct{}
		cancel   context.CancelFunc
		runErr   error
		sequence int
	}

	framedDAPMessage struct {
		Seq        int             `json:"seq"`
		Type       string          `json:"type"`
		Event      string          `json:"event"`
		Command    string          `json:"command"`
		RequestSeq int             `json:"request_seq"`
		Success    bool            `json:"success"`
		Message    string          `json:"message"`
		Body       json.RawMessage `json:"body"`
	}
)

func newFramedDAPClient(t *testing.T, configuration runtimes.Config) *framedDAPClient {
	t.Helper()
	input, requests := io.Pipe()
	responses, output := io.Pipe()

	adapter, err := ferretdap.New(input, output, ferretdap.Options{RuntimeConfig: configuration})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	client := &framedDAPClient{t: t, requests: requests, messages: make(chan framedDAPMessage, 64), stopped: make(chan struct{}), readDone: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(client.stopped)
		client.runErr = adapter.Run(ctx)
		_ = input.Close()
		_ = output.Close()
	}()
	go func() {
		defer close(client.readDone)
		defer close(client.messages)
		reader := bufio.NewReader(responses)
		for {
			content, err := protocol.ReadBaseMessage(reader)
			if err != nil {
				return
			}

			var message framedDAPMessage
			if err := json.Unmarshal(content, &message); err != nil {
				return
			}

			select {
			case client.messages <- message:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = requests.Close()
		_ = responses.Close()
		select {
		case <-client.stopped:
		case <-time.After(10 * time.Second):
			t.Error("DAP adapter failed to stop")
		}

		<-client.readDone
	})

	return client
}

func (c *framedDAPClient) send(command string, arguments any) int {
	c.t.Helper()
	c.sequence++

	payload, err := json.Marshal(map[string]any{"seq": c.sequence, "type": "request", "command": command, "arguments": arguments})
	if err != nil {
		c.t.Fatal(err)
	}

	if err := protocol.WriteBaseMessage(c.requests, payload); err != nil {
		c.t.Fatal(err)
	}

	return c.sequence
}

func (c *framedDAPClient) read() framedDAPMessage {
	c.t.Helper()
	select {
	case message, ok := <-c.messages:
		if !ok {
			c.t.Fatal("DAP stream ended unexpectedly")
		}

		return message
	case <-time.After(10 * time.Second):
		c.t.Fatal("timed out reading DAP frame")

		return framedDAPMessage{}
	}
}

func (c *framedDAPClient) response(command string, success bool) framedDAPMessage {
	c.t.Helper()

	message := c.read()
	if message.Type != "response" || message.Command != command || message.Success != success {
		c.t.Fatalf("%s response = %+v", command, message)
	}

	return message
}

func (c *framedDAPClient) event(name string) framedDAPMessage {
	c.t.Helper()

	message := c.read()
	if message.Type != "event" || message.Event != name {
		c.t.Fatalf("%s event = %+v", name, message)
	}

	return message
}

func (c *framedDAPClient) disconnect() {
	c.t.Helper()
	c.send("disconnect", map[string]any{"terminateDebuggee": true})
	c.response("disconnect", true)
	select {
	case <-c.stopped:
		if c.runErr != nil {
			c.t.Fatal(c.runErr)
		}
	case <-time.After(10 * time.Second):
		c.t.Fatal("disconnect did not settle")
	}
}
