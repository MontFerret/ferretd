package language

import (
	"context"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

// SignatureHelp returns UDF or registered overloads for the call at position.
func (s *Service) SignatureHelp(ctx context.Context, uri string, position source.Position) (*SignatureHelp, error) {
	document, resolved, err := s.resolveAt(ctx, uri, position)
	if err != nil {
		return nil, err
	}

	if resolved.Call == nil {
		return nil, nil
	}
	call := *resolved.Call

	active := activeArgument(call, resolved.Offset)
	result := &SignatureHelp{ActiveParameter: uint32(active)}

	if call.Kind == compiler.CallKindUDF && call.Target != compiler.InvalidSymbolID {
		function, ok := document.analysis.Symbol(call.Target)
		if !ok {
			return nil, nil
		}

		parameters := document.analysis.FunctionParameters(call.Target)
		values := make([]FunctionParameter, 0, len(parameters))

		for _, parameter := range parameters {
			values = append(values, FunctionParameter{Name: parameter.Name})
		}

		result.Signatures = []Signature{{
			Label:      signatureLabel(function.Name, values),
			Parameters: values,
		}}

		return result, nil
	}

	function, ok := s.functionIndex.lookup(call.Identity)
	if !ok {
		return nil, nil
	}

	result.Signatures = cloneSignatures(function.signatures)

	if len(result.Signatures) == 0 {
		return nil, nil
	}

	for index, signature := range result.Signatures {
		if signature.Variadic || len(signature.Parameters) > active {
			result.ActiveSignature = uint32(index)

			break
		}
	}

	selected := result.Signatures[result.ActiveSignature]
	if selected.Variadic && len(selected.Parameters) > 0 && active >= len(selected.Parameters) {
		result.ActiveParameter = uint32(len(selected.Parameters) - 1)
	}

	return result, nil
}
