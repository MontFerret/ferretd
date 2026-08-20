// Package lsp adapts the shared language service to the Language Server Protocol.
package lsp

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

const serverName = "ferretd"

// Server is a thin LSP adapter around the shared language service.
type Server struct {
	language *language.Service
	handler  protocol.Handler
	contexts sync.Map
}

// New creates an LSP server adapter.
func New(service *language.Service) *Server {
	if service == nil {
		service = language.New(language.Options{})
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

	return result
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

func initializationRoots(params *protocol.InitializeParams) ([]string, error) {
	if params == nil {
		return nil, nil
	}

	var values []string

	if len(params.WorkspaceFolders) > 0 {
		for _, folder := range params.WorkspaceFolders {
			path, err := source.URI(folder.URI).Path()
			if err != nil {
				return nil, err
			}

			values = append(values, path)
		}
	} else if params.RootURI != nil && *params.RootURI != "" {
		path, err := source.URI(*params.RootURI).Path()
		if err != nil {
			return nil, err
		}

		values = append(values, path)
	} else if params.RootPath != nil && *params.RootPath != "" {
		values = append(values, *params.RootPath)
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return nil, err
		}

		root := filepath.Clean(absolute)
		if _, ok := seen[root]; ok {
			continue
		}

		seen[root] = struct{}{}
		result = append(result, root)
	}

	return result, nil
}
