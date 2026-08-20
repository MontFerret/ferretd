package language

import (
	"strings"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

func terminalName(name string) string {
	if index := strings.LastIndex(name, runtime.NamespaceSeparator); index >= 0 {
		return name[index+len(runtime.NamespaceSeparator):]
	}

	return name
}

func namespaceName(name string) string {
	if index := strings.LastIndex(name, runtime.NamespaceSeparator); index >= 0 {
		return name[:index]
	}

	return ""
}
