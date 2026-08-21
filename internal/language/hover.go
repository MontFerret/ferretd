package language

import (
	"context"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

// Hover returns compiler-known symbol, type, call, and bind-parameter information.
func (s *Service) Hover(ctx context.Context, uri source.URI, position source.Position) (*Hover, error) {
	document, resolved, err := s.resolveAt(ctx, uri, position)
	if err != nil {
		return nil, err
	}

	result := &Hover{Range: resolved.Range}
	if resolved.Symbol != nil {
		symbol := *resolved.Symbol
		result.Name = symbol.Name
		result.SymbolKind = symbol.Kind

		if symbol.Kind == compiler.SymbolKindUDF {
			parameters := document.analysis.FunctionParameters(symbol.ID)
			names := make([]string, 0, len(parameters))

			for _, parameter := range parameters {
				names = append(names, parameter.Name)
			}

			result.Signature = &Signature{
				Label:      signatureLabel(symbol.Name, names),
				Parameters: names,
			}
		}

		if symbol.Type != compiler.ValueTypeUnknown {
			typeValue := symbol.Type
			result.Type = &typeValue
		}
	}

	if resolved.Call != nil && resolved.Offset >= resolved.Call.CalleeSpan.Start && resolved.Offset < resolved.Call.CalleeSpan.End && resolved.Call.Kind != compiler.CallKindUDF {
		if function, ok := s.functionIndex.lookup(resolved.Call.Identity); ok {
			for _, arity := range function.arities {
				parameters := placeholderParameters(arity, false)
				result.RegisteredSignatures = append(result.RegisteredSignatures, Signature{
					Label:      signatureLabel(function.name, parameters),
					Parameters: parameters,
				})
			}

			if function.variadic {
				parameters := placeholderParameters(1, true)
				result.RegisteredSignatures = append(result.RegisteredSignatures, Signature{
					Label:      signatureLabel(function.name, parameters),
					Parameters: parameters,
					Variadic:   true,
				})
			}
		}
	}

	if result.Type == nil && resolved.Type != nil && resolved.Type.Type != compiler.ValueTypeUnknown {
		typeValue := resolved.Type.Type
		result.Type = &typeValue
	}

	if result.Name == "" && result.Type == nil && len(result.RegisteredSignatures) == 0 {
		return nil, nil
	}

	return result, nil
}
