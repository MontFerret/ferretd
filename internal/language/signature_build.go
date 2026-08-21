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

func placeholderParameters(arity int, variadic bool) []SignatureParameter {
	parameters := make([]SignatureParameter, arity)

	for index := range parameters {
		name := fmt.Sprintf("arg%d", index+1)
		parameters[index] = SignatureParameter{Name: name, Label: name}
	}

	if variadic && len(parameters) > 0 {
		parameters[len(parameters)-1].Label += "..."
		parameters[len(parameters)-1].Variadic = true
	}

	return parameters
}

func udfSignature(name string, parameters []compiler.Symbol) Signature {
	names := make([]SignatureParameter, 0, len(parameters))
	for _, parameter := range parameters {
		names = append(names, SignatureParameter{Name: parameter.Name, Label: parameter.Name})
	}

	return Signature{
		Label:      signatureLabel(name, names),
		Parameters: names,
	}
}

func signatureLabel(name string, parameters []SignatureParameter) string {
	labels := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		labels = append(labels, parameter.Label)
	}

	return name + "(" + strings.Join(labels, ", ") + ")"
}

func signatureParameterLabel(name, typeName string, variadic bool) string {
	if variadic {
		name += "..."
	}

	if typeName == "" {
		return name
	}

	return name + ": " + typeName
}

func completionSignatureDetail(signatures []Signature) string {
	if len(signatures) == 0 {
		return "registered function"
	}

	values := make([]string, 0, len(signatures))
	for _, signature := range signatures {
		value := signature.Label
		if signature.Return != nil && signature.Return.Type != "" {
			value += " → " + signature.Return.Type
		}

		values = append(values, value)
	}

	return strings.Join(values, "\n")
}
