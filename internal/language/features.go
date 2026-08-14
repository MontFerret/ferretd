package language

import (
	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

type (
	// Location identifies a source range in one document.
	Location struct {
		URI   string
		Range source.Range
	}

	// DocumentSymbol describes one compiler declaration and optional children.
	DocumentSymbol struct {
		Name           string
		Range          source.Range
		SelectionRange source.Range
		Kind           compiler.SymbolKind
		Type           compiler.ValueType
		Children       []DocumentSymbol
	}

	// Hover describes compiler-resolved source information for adapter presentation.
	Hover struct {
		Range                source.Range
		Name                 string
		SymbolKind           compiler.SymbolKind
		Type                 *compiler.ValueType
		Signature            *Signature
		RegisteredSignatures []Signature
	}

	// CompletionKind classifies a protocol-neutral completion candidate.
	CompletionKind uint8

	// CompletionItem describes a candidate visible at a source position.
	CompletionItem struct {
		Label      string
		Detail     string
		InsertText string
		Kind       CompletionKind
	}

	// Signature describes one callable overload.
	Signature struct {
		Label      string
		Parameters []string
		Variadic   bool
	}

	// SignatureHelp describes overloads and the active call argument.
	SignatureHelp struct {
		Signatures      []Signature
		ActiveSignature uint32
		ActiveParameter uint32
	}

	// SemanticTokenKind is the protocol-neutral semantic classification.
	SemanticTokenKind uint8

	// SemanticTokenModifiers is a bitset of semantic token modifiers.
	SemanticTokenModifiers uint8

	// SemanticToken describes one single-line source token.
	SemanticToken struct {
		Range     source.Range
		Kind      SemanticTokenKind
		Modifiers SemanticTokenModifiers
	}

	// FormattingResult contains canonical source text for one complete document.
	FormattingResult struct {
		Range source.Range
		Text  string
	}
)

const (
	CompletionKindVariable CompletionKind = iota + 1
	CompletionKindFunction
	CompletionKindParameter
	CompletionKindNamespace
	CompletionKindKeyword
	CompletionKindLiteral
	CompletionKindOperator
)

const (
	SemanticTokenUnknown SemanticTokenKind = iota
	SemanticTokenNamespace
	SemanticTokenFunction
	SemanticTokenVariable
	SemanticTokenParameter
	SemanticTokenKeyword
	SemanticTokenString
	SemanticTokenNumber
	SemanticTokenComment
	SemanticTokenOperator
)

const (
	SemanticTokenDeclaration SemanticTokenModifiers = 1 << iota
	SemanticTokenReadonly
)
