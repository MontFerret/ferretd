package language

import (
	"context"
	"strings"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	"github.com/MontFerret/ferret/v2/pkg/runtime"

	"github.com/MontFerret/ferretd/internal/source"
)

type completionContext uint8

const (
	completionExpression completionContext = iota
	completionBindParameter
	completionNamespace
	completionDeclaration
	completionStatement
)

// Completion returns lexical compiler symbols, runtime environment names, and FQL language words.
func (s *Service) Completion(ctx context.Context, uri string, position source.Position) ([]CompletionItem, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return nil, err
	}

	offset := document.mapper.PositionToOffset(position)
	contextKind := completionContextAt(document.snapshot.text, document.analysis.SyntaxTokens(), offset)
	items := make(map[string]CompletionItem)

	add := func(key string, item CompletionItem) {
		if _, ok := items[key]; !ok {
			items[key] = item
		}
	}

	if contextKind == completionBindParameter {
		for _, symbol := range document.analysis.Symbols() {
			if symbol.Kind == compiler.SymbolKindBindParameter {
				add("bind\x00"+symbol.Name, CompletionItem{Label: symbol.Name, InsertText: symbol.Name, Detail: "bind parameter", Kind: CompletionKindParameter})
			}
		}

		for name := range s.params {
			add("bind\x00"+name, CompletionItem{Label: name, InsertText: name, Detail: "configured bind parameter", Kind: CompletionKindParameter})
		}

		return sortedCompletionItems(items), nil
	}

	if contextKind == completionNamespace {
		prefix := namespacePrefixAt(document.snapshot.text, offset)
		normalizedPrefix := runtime.NormalizeRegisteredName(prefix)

		for _, function := range s.functionIndex.ordered {
			if normalizedPrefix != "" && !strings.HasPrefix(function.identity, normalizedPrefix+runtime.NamespaceSeparator) {
				continue
			}

			add("registered\x00"+function.identity, CompletionItem{
				Label:      function.name,
				InsertText: terminalName(function.name),
				Detail:     function.detail,
				Kind:       CompletionKindFunction,
				Deprecated: function.deprecated,
			})
		}

		return sortedCompletionItems(items), nil
	}

	if contextKind == completionDeclaration {
		return []CompletionItem{}, nil
	}

	if contextKind == completionStatement {
		for _, item := range statementCompletionItems {
			add("word\x00"+item.Label, item)
		}

		return sortedCompletionItems(items), nil
	}

	for _, symbol := range document.analysis.VisibleSymbols(offset) {
		kind := CompletionKindVariable
		detail := symbolKindName(symbol.Kind)

		switch symbol.Kind {
		case compiler.SymbolKindUDF:
			kind = CompletionKindFunction
		case compiler.SymbolKindFunctionParameter, compiler.SymbolKindBindParameter:
			kind = CompletionKindParameter
		case compiler.SymbolKindNamespaceAlias:
			kind = CompletionKindNamespace
		}

		label := symbol.Name
		if symbol.Kind == compiler.SymbolKindBindParameter {
			label = "@" + label
		}

		add("visible\x00"+label, CompletionItem{Label: label, InsertText: label, Detail: detail, Kind: kind})
	}

	for _, function := range s.functionIndex.ordered {
		add("registered\x00"+function.identity, CompletionItem{
			Label:      function.name,
			InsertText: function.name,
			Detail:     function.detail,
			Kind:       CompletionKindFunction,
			Deprecated: function.deprecated,
		})
	}

	for _, item := range languageCompletionItems {
		add("word\x00"+item.Label, item)
	}

	return sortedCompletionItems(items), nil
}
