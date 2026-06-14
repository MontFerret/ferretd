package lsp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

func TestInitializeAdvertisesFullDocumentSync(t *testing.T) {
	server := New(language.New())

	value, err := server.initialize(nil, &protocol.InitializeParams{})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	result := value.(protocol.InitializeResult)
	options, ok := result.Capabilities.TextDocumentSync.(*protocol.TextDocumentSyncOptions)
	if !ok {
		t.Fatalf("TextDocumentSync = %T", result.Capabilities.TextDocumentSync)
	}
	if options.OpenClose == nil || !*options.OpenClose {
		t.Fatalf("OpenClose = %#v", options.OpenClose)
	}
	if options.Change == nil || *options.Change != protocol.TextDocumentSyncKindFull {
		t.Fatalf("Change = %#v", options.Change)
	}
}

func TestDocumentLifecyclePublishesDiagnostics(t *testing.T) {
	service := language.New()
	server := New(service)
	uri := documentURI(t, "query.fql")

	var published []protocol.PublishDiagnosticsParams
	glspContext := &glsp.Context{Notify: func(method string, params any) {
		if method != protocol.ServerTextDocumentPublishDiagnostics {
			t.Fatalf("notification method = %q", method)
		}
		published = append(published, params.(protocol.PublishDiagnosticsParams))
	}}

	if err := server.didOpen(glspContext, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: "ferret",
			Version:    1,
			Text:       "RETURN 1",
		},
	}); err != nil {
		t.Fatalf("didOpen: %v", err)
	}
	if len(published) != 1 || len(published[0].Diagnostics) != 0 || published[0].Version == nil || *published[0].Version != 1 {
		t.Fatalf("didOpen published = %#v", published)
	}

	if err := server.didChange(glspContext, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
			Version:                2,
		},
		ContentChanges: []any{protocol.TextDocumentContentChangeEventWhole{Text: "RETURN missing"}},
	}); err != nil {
		t.Fatalf("didChange: %v", err)
	}
	if len(published) != 2 || len(published[1].Diagnostics) == 0 || published[1].Version == nil || *published[1].Version != 2 {
		t.Fatalf("didChange published = %#v", published)
	}

	if err := server.didClose(glspContext, &protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatalf("didClose: %v", err)
	}
	if len(published) != 3 || len(published[2].Diagnostics) != 0 || published[2].Version != nil {
		t.Fatalf("didClose published = %#v", published)
	}
	if _, ok := service.GetDocument(context.Background(), uri); ok {
		t.Fatal("didClose did not remove document")
	}
}

func TestDidChangeRejectsIncrementalChanges(t *testing.T) {
	service := language.New()
	server := New(service)
	uri := documentURI(t, "query.fql")
	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, "RETURN 1"); err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}

	err := server.didChange(&glsp.Context{}, &protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri},
			Version:                2,
		},
		ContentChanges: []any{protocol.TextDocumentContentChangeEvent{
			Range: &protocol.Range{},
			Text:  "RETURN 2",
		}},
	})
	if err == nil {
		t.Fatal("didChange returned nil error")
	}

	document, _ := service.GetDocument(context.Background(), uri)
	if document.Version != 1 || document.Text != "RETURN 1" {
		t.Fatalf("document changed after rejected incremental update: %#v", document)
	}
}

func documentURI(t *testing.T, name string) string {
	t.Helper()

	uri, err := source.PathToURI(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("PathToURI: %v", err)
	}
	return uri
}
