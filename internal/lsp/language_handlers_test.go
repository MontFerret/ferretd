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
		{Range: source.Range{Start: source.Position{Line: 1, Character: 8}, End: source.Position{Line: 1, Character: 9}}, Kind: language.SemanticTokenNumber, Modifiers: language.SemanticTokenReadonly | language.SemanticTokenModifiers(1<<7)},
		{Range: source.Range{Start: source.Position{Line: 2}, End: source.Position{Line: 2, Character: 1}}, Kind: language.SemanticTokenUnknown},
		{Range: source.Range{Start: source.Position{Line: 3, Character: 1}, End: source.Position{Line: 3, Character: 4}}, Kind: language.SemanticTokenFunction},
	}

	got := encodeSemanticTokens(values)
	want := []protocol.UInteger{1, 3, 2, 2, 0, 0, 5, 1, 6, 2, 2, 1, 3, 1, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("encoded tokens = %#v, want %#v", got, want)
	}
}

func TestSemanticTokenMappingIsExplicitAndMatchesLegend(t *testing.T) {
	legend := semanticTokenLegend()
	want := []struct {
		kind language.SemanticTokenKind
		name string
	}{
		{kind: language.SemanticTokenNamespace, name: "namespace"},
		{kind: language.SemanticTokenFunction, name: "function"},
		{kind: language.SemanticTokenVariable, name: "variable"},
		{kind: language.SemanticTokenParameter, name: "parameter"},
		{kind: language.SemanticTokenKeyword, name: "keyword"},
		{kind: language.SemanticTokenString, name: "string"},
		{kind: language.SemanticTokenNumber, name: "number"},
		{kind: language.SemanticTokenComment, name: "comment"},
		{kind: language.SemanticTokenOperator, name: "operator"},
	}

	if len(legend.TokenTypes) != len(want) {
		t.Fatalf("semantic token legend = %#v", legend.TokenTypes)
	}

	for index, test := range want {
		got, ok := semanticTokenType(test.kind)
		if !ok || got != protocol.UInteger(index) || legend.TokenTypes[index] != test.name {
			t.Errorf("semantic token mapping for %d = %d, %t, legend %q", test.kind, got, ok, legend.TokenTypes[index])
		}

		if protocol.UInteger(test.kind) == got {
			t.Errorf("semantic token mapping for %q still matches the language ordinal %d", test.name, test.kind)
		}
	}

	for _, unknown := range []language.SemanticTokenKind{language.SemanticTokenUnknown, 255} {
		if got, ok := semanticTokenType(unknown); ok {
			t.Errorf("unknown semantic token kind %d mapped to %d", unknown, got)
		}
	}

	if got := semanticTokenModifierBits(language.SemanticTokenDeclaration | language.SemanticTokenReadonly | language.SemanticTokenModifiers(1<<7)); got != 3 {
		t.Fatalf("semantic modifier bits = %d, want 3", got)
	}

	legend.TokenTypes[0] = "changed"
	legend.TokenModifiers[0] = "changed"
	fresh := semanticTokenLegend()
	if fresh.TokenTypes[0] != "namespace" || fresh.TokenModifiers[0] != "declaration" {
		t.Fatalf("semantic legend exposed mutable definitions: %+v", fresh)
	}
}

func TestFormattingTabSizeClampsMalformedValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
		set   bool
		want  uint32
	}{
		{name: "missing", want: language.DefaultTabSize},
		{name: "integer", value: protocol.Integer(2), set: true, want: 2},
		{name: "float", value: float64(3), set: true, want: 3},
		{name: "zero", value: 0, set: true, want: language.DefaultTabSize},
		{name: "negative", value: -1, set: true, want: language.DefaultTabSize},
		{name: "overflow", value: float64(^uint32(0)) + 1, set: true, want: language.DefaultTabSize},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := protocol.FormattingOptions{}
			if test.set {
				options[protocol.FormattingOptionTabSize] = test.value
			}

			if got := formattingTabSize(options); got != test.want {
				t.Fatalf("formattingTabSize() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCompletionWordKindMapping(t *testing.T) {
	tests := []struct {
		kind language.CompletionKind
		want protocol.CompletionItemKind
	}{
		{kind: language.CompletionKindKeyword, want: protocol.CompletionItemKindKeyword},
		{kind: language.CompletionKindLiteral, want: protocol.CompletionItemKindValue},
		{kind: language.CompletionKindOperator, want: protocol.CompletionItemKindOperator},
	}

	for _, test := range tests {
		item := toProtocolCompletionItem(language.CompletionItem{Kind: test.kind})
		if item.Kind == nil || *item.Kind != test.want {
			t.Errorf("completion kind %d = %#v, want %d", test.kind, item.Kind, test.want)
		}
	}
}

func TestCompletionPreservesCanonicalLowercaseText(t *testing.T) {
	service := language.New(language.Options{})
	server := New(service)
	uri := documentURI(t, "completion.fql")
	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, "re"); err != nil {
		t.Fatal(err)
	}

	value, err := server.completion(nil, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Character: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, ok := value.([]protocol.CompletionItem)
	if !ok {
		t.Fatalf("completion response type = %T", value)
	}

	found := false
	for _, item := range items {
		if item.Label == "RETURN" {
			t.Fatalf("completion response contains uppercase RETURN: %+v", items)
		}

		if item.Label == "return" {
			found = true
			if item.InsertText == nil || *item.InsertText != "return" {
				t.Fatalf("return insertion text = %#v", item.InsertText)
			}
		}
	}

	if !found {
		t.Fatalf("completion response omits lowercase return: %+v", items)
	}
}

func TestStdlibMetadataMapsToCompletionSignatureHelpAndHover(t *testing.T) {
	service := language.New(language.Options{})
	server := New(service)
	uri := documentURI(t, "stdlib-metadata.fql")
	query := "RETURN average([1, 2])"
	if err := service.OpenDocument(context.Background(), uri, "ferret", 1, query); err != nil {
		t.Fatal(err)
	}
	mapper := source.NewMapper(query)

	completionValue, err := server.completion(nil, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     toProtocolPosition(mapper.OffsetToPosition(len("RETURN "))),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	completionItems := completionValue.([]protocol.CompletionItem)
	var averageCompletion *protocol.CompletionItem
	for index := range completionItems {
		if completionItems[index].Label == "average" {
			averageCompletion = &completionItems[index]

			break
		}
	}

	if averageCompletion == nil || averageCompletion.Detail == nil ||
		!strings.Contains(*averageCompletion.Detail, "average(") || strings.Contains(*averageCompletion.Detail, "arg1") {
		t.Fatalf("average completion = %+v", averageCompletion)
	}

	signature, err := server.signatureHelp(nil, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     toProtocolPosition(mapper.OffsetToPosition(strings.Index(query, "[1, 2]") + 1)),
		},
	})
	if err != nil || signature == nil || len(signature.Signatures) != 1 || len(signature.Signatures[0].Parameters) != 1 {
		t.Fatalf("average signature = %+v, %v", signature, err)
	}

	if _, ok := signature.Signatures[0].Documentation.(protocol.MarkupContent); !ok {
		t.Fatalf("signature documentation = %#v", signature.Signatures[0].Documentation)
	}

	parameter := signature.Signatures[0].Parameters[0]
	if label, ok := parameter.Label.(string); !ok || label == "arg1" {
		t.Fatalf("parameter label = %#v", parameter.Label)
	}

	if documentation, ok := parameter.Documentation.(protocol.MarkupContent); !ok ||
		documentation.Kind != protocol.MarkupKindMarkdown || !strings.Contains(documentation.Value, "Type:") {
		t.Fatalf("parameter documentation = %#v", parameter.Documentation)
	}

	hover, err := server.hover(nil, &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     toProtocolPosition(mapper.OffsetToPosition(strings.Index(query, "average"))),
	}})
	if err != nil || hover == nil {
		t.Fatalf("average hover = %+v, %v", hover, err)
	}

	content, ok := hover.Contents.(protocol.MarkupContent)
	if !ok || content.Kind != protocol.MarkupKindMarkdown ||
		!strings.Contains(content.Value, "**Parameters**") || !strings.Contains(content.Value, "**Returns**") {
		t.Fatalf("average hover Markdown = %#v", hover.Contents)
	}
}

func TestDeprecationMetadataMapsWithoutDiagnostics(t *testing.T) {
	item := toProtocolCompletionItem(language.CompletionItem{
		Label:      "old_function",
		Detail:     "old_function(value)",
		InsertText: "old_function",
		Kind:       language.CompletionKindFunction,
		Deprecated: true,
	})
	if item.Deprecated == nil || !*item.Deprecated ||
		!reflect.DeepEqual(item.Tags, []protocol.CompletionItemTag{protocol.CompletionItemTagDeprecated}) {
		t.Fatalf("deprecated completion = %+v", item)
	}

	signature := language.Signature{
		Label:       "old_function(value)",
		Parameters:  []language.FunctionParameter{{Name: "value", Type: "Any", Description: "Legacy value."}},
		Description: "Processes a legacy value.",
		Return:      &language.FunctionReturn{Type: "Any", Description: "Legacy result."},
		Throws:      []language.FunctionThrow{{Error: "TypeError", Description: "The value is invalid."}},
		Deprecated:  "Use new_function instead.",
	}
	markdown := renderHoverMarkdown(language.Hover{RegisteredSignatures: []language.Signature{signature}})
	for _, expected := range []string{"**Parameters**", "**Returns**", "**Throws**", "**Deprecated:** Use new_function instead."} {
		if !strings.Contains(markdown, expected) {
			t.Errorf("hover Markdown %q does not contain %q", markdown, expected)
		}
	}
}

func toProtocolPosition(value source.Position) protocol.Position {
	return protocol.Position{Line: value.Line, Character: value.Character}
}
