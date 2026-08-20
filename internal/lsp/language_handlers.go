package lsp

import (
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/language"
	"github.com/MontFerret/ferretd/internal/source"
)

func (s *Server) documentSymbols(
	glspContext *glsp.Context,
	params *protocol.DocumentSymbolParams,
) (any, error) {
	uri := source.URI(params.TextDocument.URI)
	values, err := s.language.DocumentSymbols(s.operationContext(glspContext), uri)
	if err != nil {
		return nil, err
	}

	result := make([]protocol.DocumentSymbol, 0, len(values))
	for _, value := range values {
		result = append(result, toProtocolDocumentSymbol(value))
	}

	return result, nil
}

func (s *Server) hover(glspContext *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	uri := source.URI(params.TextDocument.URI)
	value, err := s.language.Hover(
		s.operationContext(glspContext),
		uri,
		toSourcePosition(params.Position),
	)
	if err != nil || value == nil {
		return nil, err
	}

	rangeValue := toProtocolRange(value.Range)
	return &protocol.Hover{
		Contents: protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: renderHoverMarkdown(*value)},
		Range:    &rangeValue,
	}, nil
}

func (s *Server) definition(glspContext *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	uri := source.URI(params.TextDocument.URI)
	value, err := s.language.Definition(
		s.operationContext(glspContext),
		uri,
		toSourcePosition(params.Position),
	)
	if err != nil || value == nil {
		return nil, err
	}

	return toProtocolLocation(*value), nil
}

func (s *Server) references(
	glspContext *glsp.Context,
	params *protocol.ReferenceParams,
) ([]protocol.Location, error) {
	uri := source.URI(params.TextDocument.URI)
	values, err := s.language.References(
		s.operationContext(glspContext),
		uri,
		toSourcePosition(params.Position),
		params.Context.IncludeDeclaration,
	)
	if err != nil {
		return nil, err
	}

	result := make([]protocol.Location, 0, len(values))
	for _, value := range values {
		result = append(result, toProtocolLocation(value))
	}

	return result, nil
}

func (s *Server) completion(glspContext *glsp.Context, params *protocol.CompletionParams) (any, error) {
	uri := source.URI(params.TextDocument.URI)
	values, err := s.language.Completion(
		s.operationContext(glspContext),
		uri,
		toSourcePosition(params.Position),
	)
	if err != nil {
		return nil, err
	}

	result := make([]protocol.CompletionItem, 0, len(values))
	for _, value := range values {
		result = append(result, toProtocolCompletionItem(value))
	}

	return result, nil
}

func (s *Server) signatureHelp(
	glspContext *glsp.Context,
	params *protocol.SignatureHelpParams,
) (*protocol.SignatureHelp, error) {
	uri := source.URI(params.TextDocument.URI)
	value, err := s.language.SignatureHelp(
		s.operationContext(glspContext),
		uri,
		toSourcePosition(params.Position),
	)
	if err != nil || value == nil {
		return nil, err
	}

	activeSignature := protocol.UInteger(value.ActiveSignature)
	activeParameter := protocol.UInteger(value.ActiveParameter)
	result := &protocol.SignatureHelp{
		Signatures:      make([]protocol.SignatureInformation, 0, len(value.Signatures)),
		ActiveSignature: &activeSignature,
		ActiveParameter: &activeParameter,
	}

	for _, signature := range value.Signatures {
		information := protocol.SignatureInformation{
			Label:      signature.Label,
			Parameters: make([]protocol.ParameterInformation, 0, len(signature.Parameters)),
		}

		for _, parameter := range signature.Parameters {
			information.Parameters = append(information.Parameters, protocol.ParameterInformation{Label: parameter})
		}

		result.Signatures = append(result.Signatures, information)
	}

	return result, nil
}

func (s *Server) semanticTokensFull(
	glspContext *glsp.Context,
	params *protocol.SemanticTokensParams,
) (*protocol.SemanticTokens, error) {
	uri := source.URI(params.TextDocument.URI)
	values, err := s.language.SemanticTokens(s.operationContext(glspContext), uri)
	if err != nil {
		return nil, err
	}

	return &protocol.SemanticTokens{Data: encodeSemanticTokens(values)}, nil
}

func (s *Server) formatting(
	glspContext *glsp.Context,
	params *protocol.DocumentFormattingParams,
) ([]protocol.TextEdit, error) {
	tabSize := formattingTabSize(params.Options)
	uri := source.URI(params.TextDocument.URI)
	value, err := s.language.Format(s.operationContext(glspContext), uri, tabSize)
	if err != nil {
		return nil, err
	}

	if value == nil {
		return []protocol.TextEdit{}, nil
	}

	return []protocol.TextEdit{{Range: toProtocolRange(value.Range), NewText: value.Text}}, nil
}

func renderHoverMarkdown(value language.Hover) string {
	var sections []string
	if value.Name != "" {
		declaration := symbolKindName(value.SymbolKind) + " " + value.Name

		if value.Signature != nil {
			declaration = "FUNC " + value.Signature.Label
		} else if value.SymbolKind == compiler.SymbolKindBindParameter {
			declaration = "@" + value.Name
		}

		sections = append(sections, "```fql\n"+declaration+"\n```")

		if value.SymbolKind == compiler.SymbolKindBindParameter {
			sections = append(sections, "bind parameter")
		}
	}

	if len(value.RegisteredSignatures) > 0 {
		labels := make([]string, 0, len(value.RegisteredSignatures))

		for _, signature := range value.RegisteredSignatures {
			labels = append(labels, signature.Label)
		}

		sections = append(sections, "```fql\n"+strings.Join(labels, "\n")+"\n```")
	}

	if value.Type != nil {
		sections = append(sections, "Type: `"+valueTypeName(*value.Type)+"`")
	}

	return strings.Join(sections, "\n\n")
}

func symbolKindName(kind compiler.SymbolKind) string {
	switch kind {
	case compiler.SymbolKindFunctionParameter:
		return "parameter"
	case compiler.SymbolKindNamespaceAlias:
		return "namespace"
	case compiler.SymbolKindLoopBinding:
		return "loop binding"
	case compiler.SymbolKindMatchBinding:
		return "match binding"
	case compiler.SymbolKindCollectBinding:
		return "collect binding"
	default:
		return "binding"
	}
}

func valueTypeName(value compiler.ValueType) string {
	switch value {
	case compiler.ValueTypeAny:
		return "any"
	case compiler.ValueTypeNone:
		return "none"
	case compiler.ValueTypeInteger:
		return "integer"
	case compiler.ValueTypeFloat:
		return "float"
	case compiler.ValueTypeDuration:
		return "duration"
	case compiler.ValueTypeBoolean:
		return "boolean"
	case compiler.ValueTypeString:
		return "string"
	case compiler.ValueTypeArray:
		return "array"
	case compiler.ValueTypeObject:
		return "object"
	case compiler.ValueTypeList:
		return "list"
	case compiler.ValueTypeMap:
		return "map"
	default:
		return "unknown"
	}
}

func formattingTabSize(options protocol.FormattingOptions) uint32 {
	const maxUint32 = uint64(^uint32(0))

	value, ok := options[protocol.FormattingOptionTabSize]
	if !ok {
		return language.DefaultTabSize
	}

	switch typed := value.(type) {
	case protocol.Integer:
		if typed > 0 {
			return uint32(typed)
		}
	case float64:
		if typed > 0 && typed <= float64(maxUint32) {
			return uint32(typed)
		}
	case int:
		if typed > 0 && uint64(typed) <= maxUint32 {
			return uint32(typed)
		}
	}

	return language.DefaultTabSize
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
