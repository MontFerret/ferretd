package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
)

func TestRPCHandlerCancellationReturnsLSPCancellationCode(t *testing.T) {
	server := New(language.New(language.Options{}))
	started := make(chan struct{})
	server.handler.TextDocumentHover = func(glspContext *glsp.Context, _ *protocol.HoverParams) (*protocol.Hover, error) {
		close(started)
		ctx := server.operationContext(glspContext)
		<-ctx.Done()

		return nil, ctx.Err()
	}

	client, cleanup := rpcTestConnection(t, server)
	defer cleanup()
	initializeRPC(t, client)

	result := make(chan error, 1)
	go func() {
		var hover protocol.Hover
		result <- client.Call(context.Background(), protocol.MethodTextDocumentHover, protocol.HoverParams{}, &hover)
	}()
	<-started
	if err := client.Notify(context.Background(), protocol.MethodCancelRequest, protocol.CancelParams{
		ID: protocol.IntegerOrString{Value: protocol.Integer(1)},
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-result:
		var rpcError *jsonrpc2.Error
		if !errors.As(err, &rpcError) || rpcError.Code != requestCancelledCode {
			t.Fatalf("cancellation error = %T %v", err, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request cancellation did not complete")
	}
}

func TestRPCHandlerRejectsUnsupportedMethods(t *testing.T) {
	server := New(language.New(language.Options{}))
	client, cleanup := rpcTestConnection(t, server)
	defer cleanup()
	initializeRPC(t, client)

	var result any
	err := client.Call(context.Background(), protocol.MethodWorkspaceSymbol, struct{}{}, &result)
	var rpcError *jsonrpc2.Error
	if !errors.As(err, &rpcError) || rpcError.Code != jsonrpc2.CodeMethodNotFound {
		t.Fatalf("unsupported method error = %T %v", err, err)
	}
}

func TestRPCHandlerRequestsWaitForEarlierLifecycleNotification(t *testing.T) {
	server := New(language.New(language.Options{}))
	lifecycleStarted := make(chan struct{})
	lifecycleRelease := make(chan struct{})
	requestStarted := make(chan struct{})
	server.handler.TextDocumentDidOpen = func(*glsp.Context, *protocol.DidOpenTextDocumentParams) error {
		close(lifecycleStarted)
		<-lifecycleRelease

		return nil
	}
	server.handler.TextDocumentHover = func(*glsp.Context, *protocol.HoverParams) (*protocol.Hover, error) {
		close(requestStarted)

		return nil, nil
	}

	client, cleanup := rpcTestConnection(t, server)
	defer cleanup()
	initializeRPC(t, client)

	if err := client.Notify(context.Background(), protocol.MethodTextDocumentDidOpen, protocol.DidOpenTextDocumentParams{}); err != nil {
		t.Fatal(err)
	}
	<-lifecycleStarted

	requestDone := make(chan error, 1)
	go func() {
		var hover *protocol.Hover
		requestDone <- client.Call(context.Background(), protocol.MethodTextDocumentHover, protocol.HoverParams{}, &hover)
	}()

	select {
	case <-requestStarted:
		t.Fatal("request ran before earlier lifecycle notification completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(lifecycleRelease)
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not run after lifecycle barrier")
	}
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
}

func TestRunStreamWritesOnlyFramedProtocolResponses(t *testing.T) {
	input := bytes.NewBuffer(nil)
	writeLSPFrame(t, input, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	writeLSPFrame(t, input, `{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	writeLSPFrame(t, input, `{"jsonrpc":"2.0","method":"exit"}`)
	stream := &memoryReadWriteCloser{reader: input, closed: make(chan struct{})}

	if err := New(language.New(language.Options{})).runStream(context.Background(), stream); err != nil {
		t.Fatal(err)
	}

	output := stream.writer.String()
	if output == "" || !bytes.HasPrefix([]byte(output), []byte("Content-Length: ")) {
		t.Fatalf("stdout does not begin with protocol framing: %q", output)
	}
	if bytes.Contains([]byte(output), []byte("ferretd:")) || bytes.Contains([]byte(output), []byte("jsonrpc2:")) {
		t.Fatalf("stdout contains non-protocol text: %q", output)
	}
	if got := bytes.Count([]byte(output), []byte("Content-Length: ")); got != 2 {
		t.Fatalf("protocol response frame count = %d, output %q", got, output)
	}
}

func rpcTestConnection(t *testing.T, server *Server) (*jsonrpc2.Conn, func()) {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	handler := newRPCHandler(ctx, server)
	serverConn := jsonrpc2.NewConn(
		ctx,
		jsonrpc2.NewBufferedStream(serverSide, jsonrpc2.VSCodeObjectCodec{}),
		handler,
	)
	clientConn := jsonrpc2.NewConn(
		ctx,
		jsonrpc2.NewBufferedStream(clientSide, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (any, error) {
			return nil, nil
		}),
	)

	return clientConn, func() {
		cancel()
		handler.cancelAll()
		_ = clientConn.Close()
		_ = serverConn.Close()
		handler.waitForCompletion()
	}
}

func initializeRPC(t *testing.T, client *jsonrpc2.Conn) {
	t.Helper()

	var result protocol.InitializeResult
	if err := client.Call(context.Background(), protocol.MethodInitialize, protocol.InitializeParams{}, &result); err != nil {
		t.Fatal(err)
	}
}

type memoryReadWriteCloser struct {
	reader *bytes.Buffer
	writer bytes.Buffer
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
}

func (m *memoryReadWriteCloser) Read(buffer []byte) (int, error) {
	m.mu.Lock()
	if m.reader.Len() > 0 {
		n, err := m.reader.Read(buffer)
		m.mu.Unlock()

		return n, err
	}
	m.mu.Unlock()
	<-m.closed

	return 0, io.EOF
}

func (m *memoryReadWriteCloser) Write(buffer []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.writer.Write(buffer)
}

func (m *memoryReadWriteCloser) Close() error {
	m.once.Do(func() {
		close(m.closed)
	})

	return nil
}

func writeLSPFrame(t *testing.T, output io.Writer, body string) {
	t.Helper()

	if _, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		t.Fatal(err)
	}
}
