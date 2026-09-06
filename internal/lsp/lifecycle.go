package lsp

import (
	"context"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

func (s *Server) didOpen(glspContext *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	document := params.TextDocument
	uri := source.URI(document.URI)

	if err := s.language.OpenDocument(
		s.operationContext(glspContext),
		uri,
		document.Version,
		document.Text,
	); err != nil {
		return err
	}

	s.publishDiagnosticsAsync(s.operationContext(glspContext), s.notificationSender(glspContext), uri)

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
				return errIncrementalTextChanges
			}

			changes = append(changes, language.TextChange{Text: change.Text})
		case *protocol.TextDocumentContentChangeEvent:
			if change.Range != nil {
				return errIncrementalTextChanges
			}

			changes = append(changes, language.TextChange{Text: change.Text})
		default:
			return errUnsupportedDocumentChange
		}
	}

	document := params.TextDocument
	uri := source.URI(document.URI)

	if err := s.language.ChangeDocument(s.operationContext(glspContext), uri, document.Version, changes); err != nil {
		return err
	}

	s.publishDiagnosticsAsync(s.operationContext(glspContext), s.notificationSender(glspContext), uri)

	return nil
}

func (s *Server) didClose(glspContext *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := source.URI(params.TextDocument.URI)

	if err := s.language.CloseDocument(s.operationContext(glspContext), uri); err != nil {
		return err
	}

	s.notificationSender(glspContext)(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri.String(),
		Diagnostics: []protocol.Diagnostic{},
	})

	return nil
}

type notificationSender func(string, any)

func (s *Server) notificationSender(glspContext *glsp.Context) notificationSender {
	if glspContext != nil && glspContext.Notify != nil {
		return func(method string, params any) {
			glspContext.Notify(method, params)
		}
	}

	return func(string, any) {}
}

func (s *Server) publishDiagnosticsAsync(ctx context.Context, sendNotification notificationSender, uri source.URI) {
	go func() {
		report, err := s.language.Diagnostics(ctx, uri)
		if err != nil || !s.language.IsCurrent(context.Background(), uri, report.Snapshot) {
			return
		}

		params := protocol.PublishDiagnosticsParams{
			URI:         uri.String(),
			Diagnostics: make([]protocol.Diagnostic, 0, len(report.Items)),
		}

		if report.Version != nil && *report.Version >= 0 {
			version := protocol.UInteger(*report.Version)
			params.Version = &version
		}

		for _, diagnostic := range report.Items {
			params.Diagnostics = append(params.Diagnostics, toProtocolDiagnostic(diagnostic))
		}

		sendNotification(protocol.ServerTextDocumentPublishDiagnostics, params)
	}()
}
