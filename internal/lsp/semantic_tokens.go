package lsp

import (
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/language"
)

type (
	semanticTokenTypeDefinition struct {
		kind language.SemanticTokenKind
		name string
	}

	semanticTokenModifierDefinition struct {
		modifier language.SemanticTokenModifiers
		name     string
	}
)

var (
	semanticTokenTypeDefinitions = [...]semanticTokenTypeDefinition{
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

	semanticTokenModifierDefinitions = [...]semanticTokenModifierDefinition{
		{modifier: language.SemanticTokenDeclaration, name: "declaration"},
		{modifier: language.SemanticTokenReadonly, name: "readonly"},
	}
)

func semanticTokenLegend() protocol.SemanticTokensLegend {
	result := protocol.SemanticTokensLegend{
		TokenTypes:     make([]string, 0, len(semanticTokenTypeDefinitions)),
		TokenModifiers: make([]string, 0, len(semanticTokenModifierDefinitions)),
	}

	for _, definition := range semanticTokenTypeDefinitions {
		result.TokenTypes = append(result.TokenTypes, definition.name)
	}

	for _, definition := range semanticTokenModifierDefinitions {
		result.TokenModifiers = append(result.TokenModifiers, definition.name)
	}

	return result
}

func semanticTokenType(kind language.SemanticTokenKind) (protocol.UInteger, bool) {
	for index, definition := range semanticTokenTypeDefinitions {
		if definition.kind == kind {
			return protocol.UInteger(index), true
		}
	}

	return 0, false
}

func semanticTokenModifierBits(modifiers language.SemanticTokenModifiers) protocol.UInteger {
	var result protocol.UInteger

	for index, definition := range semanticTokenModifierDefinitions {
		if modifiers&definition.modifier != 0 {
			result |= 1 << index
		}
	}

	return result
}
