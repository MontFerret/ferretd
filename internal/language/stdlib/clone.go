package stdlib

import "github.com/MontFerret/specs/pkg/api"

func cloneReference(value *api.Reference) *api.Reference {
	result := *value
	result.Namespaces = make([]api.Namespace, len(value.Namespaces))

	for namespaceIndex, namespace := range value.Namespaces {
		result.Namespaces[namespaceIndex] = namespace
		result.Namespaces[namespaceIndex].Functions = make([]api.Function, len(namespace.Functions))

		for functionOffset, function := range namespace.Functions {
			result.Namespaces[namespaceIndex].Functions[functionOffset] = function
			result.Namespaces[namespaceIndex].Functions[functionOffset].Signatures = cloneAPISignatures(function.Signatures)
		}
	}

	return &result
}

func cloneAPISignatures(values []api.Signature) []api.Signature {
	result := make([]api.Signature, len(values))
	for index, value := range values {
		value.Parameters = cloneAPIParameters(value.Parameters)

		value.Throws = append([]api.Throw(nil), value.Throws...)
		if value.Return != nil {
			returnValue := *value.Return
			returnValue.Type = cloneAPIType(value.Return.Type)
			value.Return = &returnValue
		}

		result[index] = value
	}

	return result
}

func cloneAPIParameters(values []api.Parameter) []api.Parameter {
	result := make([]api.Parameter, len(values))
	for index, value := range values {
		value.Type = cloneAPIType(value.Type)
		result[index] = value
	}

	return result
}

func cloneAPIType(value *api.Type) *api.Type {
	if value == nil {
		return nil
	}

	result := *value
	result.Types = make([]api.Type, len(value.Types))
	for index := range value.Types {
		result.Types[index] = *cloneAPIType(&value.Types[index])
	}

	result.Element = cloneAPIType(value.Element)

	return &result
}
