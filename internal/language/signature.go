package language

import (
	"context"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

// SignatureHelp returns UDF or registered overloads for the call at position.
func (s *Service) SignatureHelp(
	ctx context.Context,
	uri source.URI,
	position source.Position,
) (*SignatureHelp, error) {
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
		result.Signatures = []Signature{udfSignature(function.Name, parameters)}

		return result, nil
	}

	function, ok := s.functions.lookup(call.Identity)
	if !ok {
		return nil, nil
	}

	result.Signatures = function.renderedSignatures(max(active+1, 1))

	if len(result.Signatures) == 0 {
		return nil, nil
	}

	variadic := -1
	for index, signature := range result.Signatures {
		if signature.Variadic {
			if variadic < 0 {
				variadic = index
			}

			continue
		}

		if len(signature.Parameters) > active {
			result.ActiveSignature = uint32(index)

			return result, nil
		}
	}

	if variadic >= 0 {
		result.ActiveSignature = uint32(variadic)

		parameters := result.Signatures[variadic].Parameters
		if len(parameters) > 0 && active >= len(parameters) {
			result.ActiveParameter = uint32(len(parameters) - 1)
		}
	}

	return result, nil
}
