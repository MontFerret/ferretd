package lsp

import (
	"strings"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	"github.com/MontFerret/ferretd/internal/language"
)

func renderHoverMarkdown(value language.Hover) string {
	var sections []string
	if value.Name != "" {
		declaration := symbolKindName(value.SymbolKind) + " " + value.Name

		if value.Signature != nil {
			declaration = "FUNC " + value.Signature.Label
		} else if value.SymbolKind == compiler.SymbolKindBindParameter {
			declaration = "@" + value.Name
		}

		sections = append(sections, "```fql\n"+declaration+"\n```")

		if value.SymbolKind == compiler.SymbolKindBindParameter {
			sections = append(sections, "bind parameter")
		}
	}

	if len(value.RegisteredSignatures) > 0 {
		labels := make([]string, 0, len(value.RegisteredSignatures))

		for _, signature := range value.RegisteredSignatures {
			labels = append(labels, signature.Label)
		}

		sections = append(sections, "```fql\n"+strings.Join(labels, "\n")+"\n```")
	}

	if value.Type != nil {
		sections = append(sections, "Type: `"+valueTypeName(*value.Type)+"`")
	}

	return strings.Join(sections, "\n\n")
}

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

func valueTypeName(value compiler.ValueType) string {
	switch value {
	case compiler.ValueTypeAny:
		return "any"
	case compiler.ValueTypeNone:
		return "none"
	case compiler.ValueTypeInteger:
		return "integer"
	case compiler.ValueTypeFloat:
		return "float"
	case compiler.ValueTypeDuration:
		return "duration"
	case compiler.ValueTypeBoolean:
		return "boolean"
	case compiler.ValueTypeString:
		return "string"
	case compiler.ValueTypeArray:
		return "array"
	case compiler.ValueTypeObject:
		return "object"
	case compiler.ValueTypeList:
		return "list"
	case compiler.ValueTypeMap:
		return "map"
	default:
		return "unknown"
	}
}
