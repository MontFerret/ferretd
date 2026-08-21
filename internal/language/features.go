package language

import (
	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

type (
	// Location identifies a source range in one document.
	Location struct {
		URI   source.URI
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
		Label         string
		Detail        string
		InsertText    string
		Kind          CompletionKind
		Documentation string
		Deprecated    bool
	}

	// Signature describes one callable overload.
	Signature struct {
		Label       string
		Parameters  []SignatureParameter
		Variadic    bool
		Description string
		Return      *SignatureReturn
		Throws      []SignatureThrow
		Deprecated  string
	}

	// SignatureParameter describes one callable parameter.
	SignatureParameter struct {
		Name        string
		Label       string
		Type        string
		Description string
		Variadic    bool
	}

	// SignatureReturn describes one documented callable result.
	SignatureReturn struct {
		Type        string
		Description string
	}

	// SignatureThrow describes one documented callable failure.
	SignatureThrow struct {
		Error       string
		Description string
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
	// CompletionKindVariable identifies a value binding.
	CompletionKindVariable CompletionKind = iota + 1
	// CompletionKindFunction identifies a callable declaration.
	CompletionKindFunction
	// CompletionKindParameter identifies a UDF parameter binding.
	CompletionKindParameter
	// CompletionKindNamespace identifies a registered function namespace.
	CompletionKindNamespace
	// CompletionKindKeyword identifies FQL syntax keywords.
	CompletionKindKeyword
	// CompletionKindLiteral identifies literal language values.
	CompletionKindLiteral
	// CompletionKindOperator identifies language operators.
	CompletionKindOperator
)

const (
	// SemanticTokenUnknown preserves compiler tokens without a known classification.
	SemanticTokenUnknown SemanticTokenKind = iota
	// SemanticTokenNamespace identifies registered function namespaces.
	SemanticTokenNamespace
	// SemanticTokenFunction identifies callable declarations and references.
	SemanticTokenFunction
	// SemanticTokenVariable identifies mutable value bindings and references.
	SemanticTokenVariable
	// SemanticTokenParameter identifies UDF parameters and their references.
	SemanticTokenParameter
	// SemanticTokenKeyword identifies FQL syntax keywords.
	SemanticTokenKeyword
	// SemanticTokenString identifies string literals.
	SemanticTokenString
	// SemanticTokenNumber identifies numeric literals.
	SemanticTokenNumber
	// SemanticTokenComment identifies source comments.
	SemanticTokenComment
	// SemanticTokenOperator identifies language operators.
	SemanticTokenOperator
)

const (
	// SemanticTokenDeclaration marks the token that declares a symbol.
	SemanticTokenDeclaration SemanticTokenModifiers = 1 << iota
	// SemanticTokenReadonly marks a symbol that cannot be reassigned.
	SemanticTokenReadonly
)
