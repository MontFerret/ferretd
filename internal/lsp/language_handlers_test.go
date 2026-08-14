package lsp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

func TestLanguageHandlerMappingsAndSemanticEncoding(t *testing.T) {
	service := language.New(language.Options{})
	server := New(service)
	uri := documentURI(t, "handlers.fql")
	query := "LET value = 1\nFUNC add(p) => value + p\nRETURN add(value)"
	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, query); err != nil {
		t.Fatal(err)
	}
	mapper := source.NewMapper(query)

	hover, err := server.hover(nil, &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     toProtocolPosition(mapper.OffsetToPosition(19)),
	}})
	if err != nil || hover == nil {
		t.Fatalf("hover = %+v, %v", hover, err)
	}
	if content, ok := hover.Contents.(protocol.MarkupContent); !ok || content.Kind != protocol.MarkupKindMarkdown {
		t.Fatalf("hover contents = %#v", hover.Contents)
	} else if !strings.Contains(content.Value, "```fql") || !strings.Contains(content.Value, "FUNC add(p)") {
		t.Fatalf("hover Markdown = %q", content.Value)
	}

	definition, err := server.definition(nil, &protocol.DefinitionParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     toProtocolPosition(mapper.OffsetToPosition(strings.LastIndex(query, "value"))),
	}})
	if err != nil || definition == nil {
		t.Fatalf("definition = %+v, %v", definition, err)
	}

	tokens, err := server.semanticTokensFull(nil, &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	})
	if err != nil || tokens == nil || len(tokens.Data) == 0 || len(tokens.Data)%5 != 0 {
		t.Fatalf("semantic tokens = %+v, %v", tokens, err)
	}

	edits, err := server.formatting(nil, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Options: protocol.FormattingOptions{
			protocol.FormattingOptionTabSize:      protocol.Integer(2),
			protocol.FormattingOptionInsertSpaces: false,
		},
	})
	if err != nil || len(edits) != 1 {
		t.Fatalf("formatting = %+v, %v", edits, err)
	}
}

func TestInitializationRootsPrecedenceAndDeduplication(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	firstURI, err := source.PathToURI(first)
	if err != nil {
		t.Fatal(err)
	}
	secondURI, err := source.PathToURI(second)
	if err != nil {
		t.Fatal(err)
	}

	rootURI := protocol.DocumentUri(secondURI)
	rootPath := second
	roots, err := initializationRoots(&protocol.InitializeParams{
		RootURI:  &rootURI,
		RootPath: &rootPath,
		WorkspaceFolders: []protocol.WorkspaceFolder{
			{URI: firstURI},
			{URI: firstURI},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != first {
		t.Fatalf("roots = %#v", roots)
	}

	roots, err = initializationRoots(&protocol.InitializeParams{RootURI: &rootURI, RootPath: &rootPath})
	if err != nil || len(roots) != 1 || roots[0] != second {
		t.Fatalf("root URI fallback = %#v, %v", roots, err)
	}

	roots, err = initializationRoots(&protocol.InitializeParams{RootPath: &rootPath})
	if err != nil || len(roots) != 1 || roots[0] != second {
		t.Fatalf("root path fallback = %#v, %v", roots, err)
	}

	invalidURI := protocol.DocumentUri("https://example.com/workspace")
	if _, err := initializationRoots(&protocol.InitializeParams{RootURI: &invalidURI}); err == nil {
		t.Fatal("non-local root URI returned nil error")
	}
}

func TestEncodeSemanticTokensUsesRelativePositions(t *testing.T) {
	values := []language.SemanticToken{
		{Range: source.Range{Start: source.Position{Line: 1, Character: 3}, End: source.Position{Line: 1, Character: 5}}, Kind: language.SemanticTokenVariable},
		{Range: source.Range{Start: source.Position{Line: 1, Character: 8}, End: source.Position{Line: 1, Character: 9}}, Kind: language.SemanticTokenNumber, Modifiers: language.SemanticTokenReadonly},
		{Range: source.Range{Start: source.Position{Line: 3, Character: 1}, End: source.Position{Line: 3, Character: 4}}, Kind: language.SemanticTokenFunction},
	}

	got := encodeSemanticTokens(values)
	want := []protocol.UInteger{1, 3, 2, 2, 0, 0, 5, 1, 6, 2, 2, 1, 3, 1, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("encoded tokens = %#v, want %#v", got, want)
	}
}

func TestFormattingTabSizeClampsMalformedValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  uint32
	}{
		{name: "integer", value: protocol.Integer(2), want: 2},
		{name: "float", value: float64(3), want: 3},
		{name: "zero", value: 0, want: 4},
		{name: "negative", value: -1, want: 4},
		{name: "overflow", value: float64(^uint32(0)) + 1, want: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := protocol.FormattingOptions{protocol.FormattingOptionTabSize: test.value}
			if got := formattingTabSize(options); got != test.want {
				t.Fatalf("formattingTabSize() = %d, want %d", got, test.want)
			}
		})
	}
}

func toProtocolPosition(value source.Position) protocol.Position {
	return protocol.Position{Line: value.Line, Character: value.Character}
}
