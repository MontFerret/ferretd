package language

import (
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	stdlibref "github.com/MontFerret/ferretd/internal/language/stdlib"
)

func defaultStdlibMetadata() functionMetadataIndex {
	defaultMetadataOnce.Do(func() {
		defaultMetadata = metadataFromStdlibReference(stdlibref.Default())
	})

	return defaultMetadata
}

func newFunctionIndex(functions *runtime.Functions, metadataIndex functionMetadataIndex) functionIndex {
	if functions == nil {
		functions = runtime.NewFunctions()
	}

	names := functions.List()
	result := functionIndex{
		ordered: make([]registeredFunction, 0, len(names)),
		byName:  make(map[string]int, len(names)),
	}

	for _, name := range names {
		identity := runtime.NormalizeRegisteredName(name)
		if _, ok := result.byName[identity]; ok {
			continue
		}

		entry := registeredFunction{name: name, namespace: namespaceName(name), identity: identity}
		metadata, hasMetadata := metadataIndex[identity]
		if hasMetadata {
			entry.name = metadata.name
			entry.namespace = metadata.namespace
		}

		if functions.A0().Has(name) {
			entry.signatures = append(entry.signatures, mergeSignature(entry.name, 0, false, metadata.signatures))
		}

		if functions.A1().Has(name) {
			entry.signatures = append(entry.signatures, mergeSignature(entry.name, 1, false, metadata.signatures))
		}

		if functions.A2().Has(name) {
			entry.signatures = append(entry.signatures, mergeSignature(entry.name, 2, false, metadata.signatures))
		}

		if functions.A3().Has(name) {
			entry.signatures = append(entry.signatures, mergeSignature(entry.name, 3, false, metadata.signatures))
		}

		if functions.A4().Has(name) {
			entry.signatures = append(entry.signatures, mergeSignature(entry.name, 4, false, metadata.signatures))
		}

		if functions.Var().Has(name) {
			entry.signatures = append(entry.signatures, mergeSignature(entry.name, 1, true, metadata.signatures))
		}

		if hasMetadata && sameSignatures(entry.signatures, metadata.signatures) {
			entry.detail = metadata.detail
			entry.deprecated = metadata.deprecated
		} else {
			entry.detail = buildFunctionDetail(entry.signatures)
			entry.deprecated = allSignaturesDeprecated(entry.signatures)
		}

		result.byName[identity] = len(result.ordered)
		result.ordered = append(result.ordered, entry)
	}

	return result
}

func mergeSignature(name string, arity int, variadic bool, metadata []Signature) Signature {
	for _, candidate := range metadata {
		if candidate.Variadic != variadic || (!variadic && len(candidate.Parameters) != arity) {
			continue
		}

		return candidate
	}

	parameters := placeholderParameters(arity, variadic)

	return Signature{
		Label:      signatureLabel(name, parameters),
		Parameters: parameters,
		Variadic:   variadic,
	}
}

func metadataFromStdlibReference(reference *stdlibref.Reference) functionMetadataIndex {
	functions := reference.Functions()
	result := make(functionMetadataIndex, len(functions))

	for _, function := range functions {
		metadata := functionMetadata{
			name:       function.Name,
			namespace:  function.Namespace,
			signatures: make([]Signature, 0, len(function.Signatures)),
		}

		for _, signature := range function.Signatures {
			parameters := make([]FunctionParameter, 0, len(signature.Parameters))
			for _, parameter := range signature.Parameters {
				parameters = append(parameters, FunctionParameter{
					Name:        parameter.Name,
					Type:        parameter.Type,
					Description: parameter.Description,
				})
			}

			if signature.Variadic && len(parameters) > 0 {
				parameters[len(parameters)-1].Name += "..."
			}

			converted := Signature{
				Label:       signatureLabel(function.Name, parameters),
				Parameters:  parameters,
				Variadic:    signature.Variadic,
				Description: signature.Description,
				Throws:      make([]FunctionThrow, 0, len(signature.Throws)),
				Deprecated:  signature.Deprecated,
			}

			if signature.Return != nil {
				converted.Return = &FunctionReturn{
					Type:        signature.Return.Type,
					Description: signature.Return.Description,
				}
			}

			for _, thrown := range signature.Throws {
				converted.Throws = append(converted.Throws, FunctionThrow{
					Error:       thrown.Error,
					Description: thrown.Description,
				})
			}

			metadata.signatures = append(metadata.signatures, converted)
		}

		metadata.detail = buildFunctionDetail(metadata.signatures)
		metadata.deprecated = allSignaturesDeprecated(metadata.signatures)
		result[runtime.NormalizeRegisteredName(function.Name)] = metadata
	}

	return result
}

func sameSignatures(left, right []Signature) bool {
	if len(left) != len(right) {
		return false
	}

	for index := range left {
		if left[index].Label != right[index].Label || left[index].Variadic != right[index].Variadic {
			return false
		}
	}

	return true
}

func buildFunctionDetail(signatures []Signature) string {
	labels := make([]string, 0, len(signatures))
	for _, signature := range signatures {
		labels = append(labels, signature.Label)
	}

	return "registered function: " + joinSignatureLabels(labels)
}

func allSignaturesDeprecated(signatures []Signature) bool {
	if len(signatures) == 0 {
		return false
	}

	for _, signature := range signatures {
		if signature.Deprecated == "" {
			return false
		}
	}

	return true
}
