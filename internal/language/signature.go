package language

import (
	"context"
	"fmt"
	"strings"

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
		names := make([]string, 0, len(parameters))

		for _, parameter := range parameters {
			names = append(names, parameter.Name)
		}

		result.Signatures = []Signature{{
			Label:      signatureLabel(function.Name, names),
			Parameters: names,
		}}

		return result, nil
	}

	function, ok := s.functionIndex.lookup(call.Identity)
	if !ok {
		return nil, nil
	}

	for _, arity := range function.arities {
		parameters := placeholderParameters(arity, false)
		result.Signatures = append(result.Signatures, Signature{
			Label:      signatureLabel(function.name, parameters),
			Parameters: parameters,
		})
	}

	if function.variadic {
		parameters := placeholderParameters(maxInt(active+1, 1), true)
		result.Signatures = append(result.Signatures, Signature{
			Label:      signatureLabel(function.name, parameters),
			Parameters: parameters,
			Variadic:   true,
		})
	}

	if len(result.Signatures) == 0 {
		return nil, nil
	}

	for index, signature := range result.Signatures {
		if signature.Variadic || len(signature.Parameters) > active {
			result.ActiveSignature = uint32(index)

			break
		}
	}

	return result, nil
}

func activeArgument(call compiler.Call, offset int) int {
	for index, span := range call.ArgumentSpans {
		if offset <= span.End {
			return index
		}
	}

	return len(call.ArgumentSpans)
}

func placeholderParameters(arity int, variadic bool) []string {
	parameters := make([]string, arity)

	for index := range parameters {
		parameters[index] = fmt.Sprintf("arg%d", index+1)
	}

	if variadic && len(parameters) > 0 {
		parameters[len(parameters)-1] += "..."
	}

	return parameters
}

func signatureLabel(name string, parameters []string) string {
	return name + "(" + strings.Join(parameters, ", ") + ")"
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}

	return right
}
