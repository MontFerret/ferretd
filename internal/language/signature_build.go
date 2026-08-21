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

func udfSignature(name string, parameters []compiler.Symbol) Signature {
	names := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		names = append(names, parameter.Name)
	}

	return Signature{
		Label:      signatureLabel(name, names),
		Parameters: names,
	}
}

func signatureLabel(name string, parameters []string) string {
	return name + "(" + strings.Join(parameters, ", ") + ")"
}
