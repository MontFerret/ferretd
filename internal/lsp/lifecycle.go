package lsp

import (
	"context"
	"errors"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
)

func (s *Server) didOpen(glspContext *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	document := params.TextDocument
	if err := s.language.OpenDocument(
		s.operationContext(glspContext),
		document.URI,
		document.LanguageID,
		document.Version,
		document.Text,
	); err != nil {
		return err
	}

	s.publishDiagnosticsAsync(s.operationContext(glspContext), s.notifier(glspContext), document.URI)

	return nil
}

func (s *Server) didChange(glspContext *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
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
	if err := s.language.ChangeDocument(s.operationContext(glspContext), document.URI, document.Version, changes); err != nil {
		return err
	}

	s.publishDiagnosticsAsync(s.operationContext(glspContext), s.notifier(glspContext), document.URI)

	return nil
}

func (s *Server) didClose(glspContext *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	if err := s.language.CloseDocument(s.operationContext(glspContext), uri); err != nil {
		return err
	}

	s.notifier(glspContext)(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})

	return nil
}

type notifyFunc func(string, any)

func (s *Server) notifier(glspContext *glsp.Context) notifyFunc {
	if glspContext != nil && glspContext.Notify != nil {
		return func(method string, params any) {
			glspContext.Notify(method, params)
		}
	}

	return func(string, any) {}
}

func (s *Server) publishDiagnosticsAsync(ctx context.Context, notify notifyFunc, uri string) {
	go func() {
		report, err := s.language.Diagnostics(ctx, uri)
		if err != nil || !s.language.IsCurrent(context.Background(), uri, report.Snapshot) {
			return
		}

		params := protocol.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: make([]protocol.Diagnostic, 0, len(report.Items)),
		}

		if report.Version != nil && *report.Version >= 0 {
			version := protocol.UInteger(*report.Version)
			params.Version = &version
		}

		for _, diagnostic := range report.Items {
			params.Diagnostics = append(params.Diagnostics, toProtocolDiagnostic(diagnostic))
		}

		notify(protocol.ServerTextDocumentPublishDiagnostics, params)
	}()
}
