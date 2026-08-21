package language

import "github.com/MontFerret/ferret/v2/pkg/compiler"

func symbolKindName(kind compiler.SymbolKind) string {
	switch kind {
	case compiler.SymbolKindFunctionParameter:
		return "parameter"
	case compiler.SymbolKindNamespaceAlias:
		return "namespace"
	case compiler.SymbolKindLoopBinding:
		return "loop binding"
	case compiler.SymbolKindMatchBinding:
		return "match binding"
	case compiler.SymbolKindCollectBinding:
		return "collect binding"
	default:
		return "binding"
	}
}
