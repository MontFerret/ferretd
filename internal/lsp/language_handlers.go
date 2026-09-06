package lsp

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

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

		if documentation := language.RenderSignatureDocumentation(signature); documentation != "" {
			information.Documentation = protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: documentation,
			}
		}

		for _, parameter := range signature.Parameters {
			value := protocol.ParameterInformation{Label: parameter.Label}
			if parameter.Description != "" {
				value.Documentation = parameter.Description
			}

			information.Parameters = append(information.Parameters, value)
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
