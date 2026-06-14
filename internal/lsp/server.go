// Package lsp adapts the shared language service to the Language Server Protocol.
package lsp

import (
	"context"
	"errors"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

const serverName = "ferretd"

// Server is a thin LSP adapter around the shared language service.
type Server struct {
	language    *language.Service
	handler     protocol.Handler
	lifecycleMu sync.Mutex
}

// New creates an LSP server adapter.
func New(service *language.Service) *Server {
	if service == nil {
		service = language.New()
	}

	result := &Server{language: service}
	result.handler = protocol.Handler{
		Initialize:            result.initialize,
		Initialized:           result.initialized,
		Shutdown:              result.shutdown,
		Exit:                  result.exit,
		TextDocumentDidOpen:   result.didOpen,
		TextDocumentDidChange: result.didChange,
		TextDocumentDidClose:  result.didClose,
	}

	return result
}

// Run serves LSP messages over stdin and stdout until the connection closes.
func (s *Server) Run(ctx context.Context) error {
	server := glspserver.NewServer(&s.handler, serverName, false)
	server.Context = ctx
	return server.RunStdio()
}

func (s *Server) initialize(_ *glsp.Context, _ *protocol.InitializeParams) (any, error) {
	capabilities := s.handler.CreateServerCapabilities()
	full := protocol.TextDocumentSyncKindFull
	capabilities.TextDocumentSync = &protocol.TextDocumentSyncOptions{
		OpenClose: &protocol.True,
		Change:    &full,
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

func (s *Server) didOpen(glspContext *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	document := params.TextDocument
	if err := s.language.OpenDocument(
		context.Background(),
		document.URI,
		document.LanguageID,
		document.Version,
		document.Text,
	); err != nil {
		return err
	}

	return s.publishDiagnostics(glspContext, document.URI)
}

func (s *Server) didChange(glspContext *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	changes := make([]language.TextChange, 0, len(params.ContentChanges))
	for _, contentChange := range params.ContentChanges {
		switch change := contentChange.(type) {
		case protocol.TextDocumentContentChangeEventWhole:
			changes = append(changes, language.TextChange{Text: change.Text})
		case *protocol.TextDocumentContentChangeEventWhole:
			changes = append(changes, language.TextChange{Text: change.Text})
		case protocol.TextDocumentContentChangeEvent:
			if change.Range != nil {
				return errors.New("incremental text document changes are not supported")
			}
			changes = append(changes, language.TextChange{Text: change.Text})
		case *protocol.TextDocumentContentChangeEvent:
			if change.Range != nil {
				return errors.New("incremental text document changes are not supported")
			}
			changes = append(changes, language.TextChange{Text: change.Text})
		default:
			return errors.New("unsupported text document change")
		}
	}

	document := params.TextDocument
	if err := s.language.ChangeDocument(context.Background(), document.URI, document.Version, changes); err != nil {
		return err
	}

	return s.publishDiagnostics(glspContext, document.URI)
}

func (s *Server) didClose(glspContext *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	uri := params.TextDocument.URI
	if err := s.language.CloseDocument(context.Background(), uri); err != nil {
		return err
	}

	s.notify(glspContext, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})

	return nil
}

func (s *Server) publishDiagnostics(glspContext *glsp.Context, uri string) error {
	diagnostics, err := s.language.Diagnostics(context.Background(), uri)
	if err != nil {
		return err
	}

	params := protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: make([]protocol.Diagnostic, 0, len(diagnostics)),
	}
	if document, ok := s.language.GetDocument(context.Background(), uri); ok && document.Version >= 0 {
		version := protocol.UInteger(document.Version)
		params.Version = &version
	}

	for _, diagnostic := range diagnostics {
		params.Diagnostics = append(params.Diagnostics, toProtocolDiagnostic(diagnostic))
	}
	s.notify(glspContext, params)

	return nil
}

func (s *Server) notify(glspContext *glsp.Context, params protocol.PublishDiagnosticsParams) {
	if glspContext != nil && glspContext.Notify != nil {
		glspContext.Notify(protocol.ServerTextDocumentPublishDiagnostics, params)
	}
}

func toProtocolDiagnostic(diagnostic language.Diagnostic) protocol.Diagnostic {
	severity := protocol.DiagnosticSeverityError
	result := protocol.Diagnostic{
		Range:    toProtocolRange(diagnostic.Range),
		Severity: &severity,
		Message:  diagnostic.Message,
	}
	if diagnostic.Source != "" {
		result.Source = &diagnostic.Source
	}
	if diagnostic.Code != "" {
		result.Code = &protocol.IntegerOrString{Value: diagnostic.Code}
	}

	for _, related := range diagnostic.RelatedInformation {
		result.RelatedInformation = append(result.RelatedInformation, protocol.DiagnosticRelatedInformation{
			Location: protocol.Location{
				URI:   related.URI,
				Range: toProtocolRange(related.Range),
			},
			Message: related.Message,
		})
	}

	return result
}

func toProtocolRange(value source.Range) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{
			Line:      value.Start.Line,
			Character: value.Start.Character,
		},
		End: protocol.Position{
			Line:      value.End.Line,
			Character: value.End.Character,
		},
	}
}
