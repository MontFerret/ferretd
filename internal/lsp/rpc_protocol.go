package lsp

import (
	"github.com/sourcegraph/jsonrpc2"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func requestCancelledError() *jsonrpc2.Error {
	return &jsonrpc2.Error{Code: requestCancelledCode, Message: "request cancelled"}
}

func isLifecycleMethod(method string) bool {
	switch method {
	case protocol.MethodInitialize, protocol.MethodInitialized, protocol.MethodShutdown, protocol.MethodExit,
		protocol.MethodTextDocumentDidOpen, protocol.MethodTextDocumentDidChange, protocol.MethodTextDocumentDidClose:
		return true
	default:
		return false
	}
}
