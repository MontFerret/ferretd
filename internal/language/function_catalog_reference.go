package language

import (
	"sort"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/specs/pkg/api"
)

func newReferenceFunctions(reference *api.Reference) []functionSymbol {
	result := make([]functionSymbol, 0)

	for _, namespace := range reference.Namespaces {
		for _, function := range namespace.Functions {
			name := function.Name
			if namespace.Name != "" {
				name = namespace.Name + runtime.NamespaceSeparator + name
			}

			result = append(result, functionSymbol{
				name:       name,
				identity:   runtime.NormalizeRegisteredName(name),
				authored:   true,
				signatures: newReferenceSignatures(name, function.Signatures),
			})
		}
	}

	return result
}

func newReferenceSignatures(name string, values []api.Signature) []Signature {
	result := make([]Signature, 0, len(values))

	for _, value := range values {
		parameters := make([]SignatureParameter, 0, len(value.Parameters))
		for index, parameter := range value.Parameters {
			variadic := value.Variadic && index == len(value.Parameters)-1
			typeName := renderAPIType(parameter.Type)
			parameters = append(parameters, SignatureParameter{
				Name:        parameter.Name,
				Label:       signatureParameterLabel(parameter.Name, typeName, variadic),
				Type:        typeName,
				Description: parameter.Description,
				Variadic:    variadic,
			})
		}

		signature := Signature{
			Label:       signatureLabel(name, parameters),
			Parameters:  parameters,
			Variadic:    value.Variadic,
			Description: value.Description,
			Deprecated:  value.Deprecated,
			Throws:      newSignatureThrows(value.Throws),
		}

		if value.Return != nil {
			signature.Return = &SignatureReturn{
				Type:        renderAPIType(value.Return.Type),
				Description: value.Return.Description,
			}
		}

		result = append(result, signature)
	}

	return result
}

func newSignatureThrows(values []api.Throw) []SignatureThrow {
	result := make([]SignatureThrow, 0, len(values))
	for _, value := range values {
		result = append(result, SignatureThrow{Error: value.Error, Description: value.Description})
	}

	return result
}

func (c *FunctionCatalog) mergeReference(reference *api.Reference) []CatalogWarning {
	referenceFunctions := newReferenceFunctions(reference)
	byName := make(map[string]functionSymbol, len(referenceFunctions))
	for _, function := range referenceFunctions {
		byName[function.identity] = function
	}

	warnings := make([]CatalogWarning, 0)
	for index := range c.ordered {
		function := &c.ordered[index]

		referenceFunction, ok := byName[function.identity]
		if !ok {
			warnings = append(warnings, CatalogWarning{Kind: CatalogWarningRuntimeOnly, Name: function.name})

			continue
		}

		function.authored = true
		function.signatures = newSignaturesWithName(function.name, referenceFunction.signatures)
		function.cacheCompletion()
		delete(byName, function.identity)
	}

	for _, function := range byName {
		warnings = append(warnings, CatalogWarning{Kind: CatalogWarningReferenceOnly, Name: function.name})
	}

	sort.Slice(warnings, func(left, right int) bool {
		if warnings[left].Kind != warnings[right].Kind {
			return warnings[left].Kind < warnings[right].Kind
		}

		return warnings[left].Name < warnings[right].Name
	})

	return warnings
}

func newSignaturesWithName(name string, values []Signature) []Signature {
	result := make([]Signature, 0, len(values))
	for _, value := range values {
		value.Label = signatureLabel(name, value.Parameters)
		result = append(result, value)
	}

	return result
}
