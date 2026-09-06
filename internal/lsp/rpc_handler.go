package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const requestCancelledCode = -32800

// rpcHandler preserves lifecycle ordering while allowing independent requests to run concurrently.
type rpcHandler struct {
	server *Server
	ctx    context.Context

	mu       sync.Mutex
	tail     chan struct{}
	inFlight map[jsonrpc2.ID]context.CancelFunc
	wait     sync.WaitGroup
}

func newRPCHandler(ctx context.Context, server *Server) *rpcHandler {
	tail := make(chan struct{})
	close(tail)

	return &rpcHandler{
		server:   server,
		ctx:      ctx,
		tail:     tail,
		inFlight: make(map[jsonrpc2.ID]context.CancelFunc),
	}
}

func (h *rpcHandler) Handle(_ context.Context, conn *jsonrpc2.Conn, request *jsonrpc2.Request) {
	if request.Method == string(protocol.MethodCancelRequest) {
		h.cancel(request)

		return
	}

	requestCtx := h.ctx
	var cancel context.CancelFunc

	if !request.Notif {
		requestCtx, cancel = context.WithCancel(h.ctx)
		h.mu.Lock()
		h.inFlight[request.ID] = cancel
		h.mu.Unlock()
	}

	h.mu.Lock()
	barrier := h.tail
	var lifecycleDone chan struct{}

	if isLifecycleMethod(request.Method) {
		lifecycleDone = make(chan struct{})
		h.tail = lifecycleDone
	}

	h.wait.Add(1)
	h.mu.Unlock()

	go h.dispatch(requestCtx, cancel, conn, request, barrier, lifecycleDone)
}

func (h *rpcHandler) dispatch(
	ctx context.Context,
	cancel context.CancelFunc,
	conn *jsonrpc2.Conn,
	request *jsonrpc2.Request,
	barrier <-chan struct{},
	lifecycleDone chan struct{},
) {
	defer h.wait.Done()

	if lifecycleDone != nil {
		defer close(lifecycleDone)
	}

	if cancel != nil {
		defer cancel()
		defer h.remove(request.ID)
	}

	select {
	case <-ctx.Done():
		h.replyError(conn, request, requestCancelledError())

		return
	case <-barrier:
	}

	if request.Method == protocol.MethodExit {
		_ = conn.Close()

		return
	}

	params := json.RawMessage(nil)

	if request.Params != nil {
		params = append(params, (*request.Params)...)
	}

	glspContext := &glsp.Context{
		Method: request.Method,
		Params: params,
		Notify: func(method string, params any) {
			_ = conn.Notify(h.ctx, method, params)
		},
	}

	h.server.contexts.Store(glspContext, ctx)
	result, validMethod, validParams, err := h.server.handler.Handle(glspContext)
	h.server.contexts.Delete(glspContext)

	if request.Notif {
		return
	}

	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		h.replyError(conn, request, requestCancelledError())

		return
	}

	if !validMethod {
		h.replyError(conn, request, &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: "method not found"})

		return
	}

	if !validParams {
		h.replyError(conn, request, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: "invalid params"})

		return
	}

	if err != nil {
		h.replyError(conn, request, &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: err.Error()})

		return
	}

	_ = conn.Reply(h.ctx, request.ID, result)
}

func (h *rpcHandler) cancel(request *jsonrpc2.Request) {
	if request.Params == nil {
		return
	}

	var params struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(*request.Params, &params); err != nil {
		return
	}

	var id jsonrpc2.ID
	if err := id.UnmarshalJSON(params.ID); err != nil {
		return
	}

	h.mu.Lock()
	cancel := h.inFlight[id]
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (h *rpcHandler) remove(id jsonrpc2.ID) {
	h.mu.Lock()
	delete(h.inFlight, id)
	h.mu.Unlock()
}

func (h *rpcHandler) replyError(conn *jsonrpc2.Conn, request *jsonrpc2.Request, err *jsonrpc2.Error) {
	if request.Notif {
		return
	}

	_ = conn.ReplyWithError(h.ctx, request.ID, err)
}

func (h *rpcHandler) cancelAll() {
	h.mu.Lock()
	for _, cancel := range h.inFlight {
		cancel()
	}

	h.mu.Unlock()
}

func (h *rpcHandler) waitForCompletion() {
	h.wait.Wait()
}
