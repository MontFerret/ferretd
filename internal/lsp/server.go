// Package lsp adapts the shared language service to the Language Server Protocol.
package lsp

import (
	"context"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
)

const serverName = "ferretd"

// Server is a thin LSP adapter around the shared language service.
type Server struct {
	language *language.Service
	handler  protocol.Handler
	contexts sync.Map
}

// New creates an LSP server adapter around the supplied language service.
// It returns an error when the language service is nil.
func New(service *language.Service) (*Server, error) {
	if service == nil {
		return nil, errNilLanguageService
	}

	result := &Server{language: service}
	result.handler = protocol.Handler{
		Initialize:                     result.initialize,
		Initialized:                    result.initialized,
		Shutdown:                       result.shutdown,
		Exit:                           result.exit,
		TextDocumentDidOpen:            result.didOpen,
		TextDocumentDidChange:          result.didChange,
		TextDocumentDidClose:           result.didClose,
		TextDocumentDocumentSymbol:     result.documentSymbols,
		TextDocumentHover:              result.hover,
		TextDocumentDefinition:         result.definition,
		TextDocumentReferences:         result.references,
		TextDocumentCompletion:         result.completion,
		TextDocumentSignatureHelp:      result.signatureHelp,
		TextDocumentSemanticTokensFull: result.semanticTokensFull,
		TextDocumentFormatting:         result.formatting,
	}

	return result, nil
}

func (s *Server) initialize(glspContext *glsp.Context, params *protocol.InitializeParams) (any, error) {
	ctx := s.operationContext(glspContext)

	roots, err := initializationRoots(params)
	if err != nil {
		return nil, err
	}

	for _, root := range roots {
		if err := s.language.OpenWorkspace(ctx, root); err != nil {
			return nil, err
		}
	}

	full := protocol.TextDocumentSyncKindFull
	capabilities := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: &protocol.True,
			Change:    &full,
		},
		DocumentSymbolProvider:     true,
		HoverProvider:              true,
		DefinitionProvider:         true,
		ReferencesProvider:         true,
		DocumentFormattingProvider: true,
		CompletionProvider:         &protocol.CompletionOptions{TriggerCharacters: []string{"@", ":"}},
		SignatureHelpProvider:      &protocol.SignatureHelpOptions{TriggerCharacters: []string{"(", ","}},
		SemanticTokensProvider: &protocol.SemanticTokensOptions{
			Legend: semanticTokenLegend(),
			Full:   true,
		},
		Workspace: &protocol.ServerCapabilitiesWorkspace{
			WorkspaceFolders: &protocol.WorkspaceFoldersServerCapabilities{Supported: &protocol.True},
		},
	}

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name: serverName,
		},
	}, nil
}

func (s *Server) initialized(*glsp.Context, *protocol.InitializedParams) error {
	return nil
}

func (s *Server) shutdown(*glsp.Context) error {
	return nil
}

func (s *Server) exit(*glsp.Context) error {
	return nil
}

func (s *Server) operationContext(glspContext *glsp.Context) context.Context {
	if glspContext != nil {
		if value, ok := s.contexts.Load(glspContext); ok {
			return value.(context.Context)
		}
	}

	return context.Background()
}
