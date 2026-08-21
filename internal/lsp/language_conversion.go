package lsp

import (
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

func toProtocolDiagnostic(diagnostic diagnostic.Diagnostic) protocol.Diagnostic {
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
				URI:   related.URI.String(),
				Range: toProtocolRange(related.Range),
			},
			Message: related.Message,
		})
	}

	return result
}

func toProtocolDocumentSymbol(value language.DocumentSymbol) protocol.DocumentSymbol {
	result := protocol.DocumentSymbol{
		Name:           value.Name,
		Kind:           toProtocolSymbolKind(value.Kind),
		Range:          toProtocolRange(value.Range),
		SelectionRange: toProtocolRange(value.SelectionRange),
		Children:       make([]protocol.DocumentSymbol, 0, len(value.Children)),
	}

	for _, child := range value.Children {
		result.Children = append(result.Children, toProtocolDocumentSymbol(child))
	}

	return result
}

func toProtocolSymbolKind(value compiler.SymbolKind) protocol.SymbolKind {
	switch value {
	case compiler.SymbolKindUDF:
		return protocol.SymbolKindFunction
	case compiler.SymbolKindNamespaceAlias:
		return protocol.SymbolKindNamespace
	default:
		return protocol.SymbolKindVariable
	}
}

func toProtocolLocation(value language.Location) protocol.Location {
	return protocol.Location{URI: value.URI.String(), Range: toProtocolRange(value.Range)}
}

func toSourcePosition(value protocol.Position) source.Position {
	return source.Position{Line: value.Line, Character: value.Character}
}

func toProtocolRange(value source.Range) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: value.Start.Line, Character: value.Start.Character},
		End:   protocol.Position{Line: value.End.Line, Character: value.End.Character},
	}
}

func toProtocolCompletionItem(value language.CompletionItem) protocol.CompletionItem {
	kind := protocol.CompletionItemKindVariable
	switch value.Kind {
	case language.CompletionKindFunction:
		kind = protocol.CompletionItemKindFunction
	case language.CompletionKindParameter:
		kind = protocol.CompletionItemKindVariable
	case language.CompletionKindNamespace:
		kind = protocol.CompletionItemKindModule
	case language.CompletionKindKeyword:
		kind = protocol.CompletionItemKindKeyword
	case language.CompletionKindLiteral:
		kind = protocol.CompletionItemKindValue
	case language.CompletionKindOperator:
		kind = protocol.CompletionItemKindOperator
	}

	return protocol.CompletionItem{
		Label:      value.Label,
		Kind:       &kind,
		Detail:     &value.Detail,
		InsertText: &value.InsertText,
	}
}

func encodeSemanticTokens(values []language.SemanticToken) []protocol.UInteger {
	result := make([]protocol.UInteger, 0, len(values)*5)
	var previousLine uint32
	var previousStart uint32

	for _, value := range values {
		tokenType, ok := semanticTokenType(value.Kind)
		if !ok {
			continue
		}

		line := value.Range.Start.Line
		start := value.Range.Start.Character
		deltaLine := line - previousLine
		deltaStart := start
		if deltaLine == 0 {
			deltaStart -= previousStart
		}

		length := value.Range.End.Character - value.Range.Start.Character
		result = append(result,
			protocol.UInteger(deltaLine),
			protocol.UInteger(deltaStart),
			protocol.UInteger(length),
			tokenType,
			semanticTokenModifierBits(value.Modifiers),
		)
		previousLine = line
		previousStart = start
	}

	return result
}
