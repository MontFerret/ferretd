package language

import (
	"fmt"
	"strings"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
)

func activeArgument(call compiler.Call, offset int) int {
	for index, span := range call.ArgumentSpans {
		if offset <= span.End {
			return index
		}
	}

	return len(call.ArgumentSpans)
}

func placeholderParameters(arity int, variadic bool) []FunctionParameter {
	parameters := make([]FunctionParameter, arity)

	for index := range parameters {
		parameters[index].Name = fmt.Sprintf("arg%d", index+1)
	}

	if variadic && len(parameters) > 0 {
		parameters[len(parameters)-1].Name += "..."
	}

	return parameters
}

func signatureLabel(name string, parameters []FunctionParameter) string {
	names := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		names = append(names, parameter.Name)
	}

	return name + "(" + strings.Join(names, ", ") + ")"
}

func joinSignatureLabels(labels []string) string {
	return strings.Join(labels, " | ")
}

func cloneSignatures(signatures []Signature) []Signature {
	result := make([]Signature, len(signatures))

	for index, signature := range signatures {
		result[index] = signature
		result[index].Parameters = append([]FunctionParameter(nil), signature.Parameters...)
		result[index].Throws = append([]FunctionThrow(nil), signature.Throws...)

		if signature.Return != nil {
			value := *signature.Return
			result[index].Return = &value
		}
	}

	return result
}
