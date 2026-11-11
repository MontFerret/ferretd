package dap

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	protocol "github.com/google/go-dap"
)

type (
	failedDAPWriter struct{ cause error }

	gatedDAPWriter struct {
		io.Writer
		entered chan struct{}
		release chan struct{}
		once    sync.Once
	}
)

func (w failedDAPWriter) Write([]byte) (int, error) {
	return 0, w.cause
}

func (w *gatedDAPWriter) Write(data []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}

	<-w.release

	return w.Writer.Write(data)
}

func (w *gatedDAPWriter) unblock() {
	w.once.Do(func() { close(w.release) })
}

func (c *testClient) gateWrites() *gatedDAPWriter {
	c.t.Helper()
	c.server.writeMu.Lock()
	writer := &gatedDAPWriter{
		Writer:  c.server.writer,
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	c.server.writer = writer
	c.server.writeMu.Unlock()
	c.t.Cleanup(writer.unblock)

	return writer
}

func (c *testClient) readRaw() map[string]json.RawMessage {
	c.t.Helper()
	result := make(chan map[string]json.RawMessage, 1)
	go func() {
		content, err := protocol.ReadBaseMessage(c.output)
		if err != nil {
			result <- nil

			return
		}

		var message map[string]json.RawMessage
		_ = json.Unmarshal(content, &message)
		result <- message
	}()
	select {
	case message := <-result:
		if message == nil {
			c.t.Fatal("could not read framed JSON")
		}

		return message
	case <-time.After(5 * time.Second):
		c.t.Fatal("timed out reading framed JSON")

		return nil
	}
}
