package lsp

import (
	"context"
	"io"
	"log"

	"github.com/sourcegraph/jsonrpc2"
)

// Run serves LSP messages over stdin and stdout until cancellation or disconnect.
func (s *Server) Run(ctx context.Context) error {
	stream := newStdioReadWriteCloser()

	return s.runStream(ctx, stream)
}

func (s *Server) runStream(ctx context.Context, stream io.ReadWriteCloser) error {
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	handler := newRPCHandler(connectionCtx, s)
	connection := jsonrpc2.NewConn(
		connectionCtx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		handler,
		jsonrpc2.SetLogger(log.New(io.Discard, "", 0)),
	)

	select {
	case <-ctx.Done():
		cancel()
		handler.cancelAll()
		_ = connection.Close()
	case <-connection.DisconnectNotify():
		cancel()
		handler.cancelAll()
	}

	handler.waitForCompletion()

	return nil
}
