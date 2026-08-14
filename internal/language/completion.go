package language

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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

			add("registered\x00"+function.identity, CompletionItem{Label: function.name, InsertText: terminalName(function.name), Detail: "registered function", Kind: CompletionKindFunction})
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
		add("registered\x00"+function.identity, CompletionItem{Label: function.name, InsertText: function.name, Detail: "registered function", Kind: CompletionKindFunction})
	}

	for _, item := range languageCompletionItems {
		add("word\x00"+item.Label, item)
	}

	return sortedCompletionItems(items), nil
}

func completionContextAt(text string, tokens []compiler.SyntaxToken, offset int) completionContext {
	if offset <= 0 || len(text) == 0 {
		return completionStatement
	}

	if offset > len(text) {
		offset = len(text)
	}

	start := offset
	for start > 0 {
		r, size := utf8LastRune(text[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		start -= size
	}

	if start > 0 && text[start-1] == '@' {
		return completionBindParameter
	}

	separatorLength := len(runtime.NamespaceSeparator)
	if start >= separatorLength && text[start-separatorLength:start] == runtime.NamespaceSeparator {
		return completionNamespace
	}

	var previous compiler.SyntaxToken
	for _, token := range tokens {
		if token.Span.End > offset {
			break
		}
		previous = token
	}

	if previous.Span.End > previous.Span.Start {
		switch previous.Word {
		case compiler.SyntaxWordLet, compiler.SyntaxWordVar, compiler.SyntaxWordFunc, compiler.SyntaxWordAs:
			return completionDeclaration
		case compiler.SyntaxWordReturn, compiler.SyntaxWordFilter, compiler.SyntaxWordSort,
			compiler.SyntaxWordLimit, compiler.SyntaxWordCollect, compiler.SyntaxWordFor, compiler.SyntaxWordWhile:
			return completionExpression
		default:
			value := text[previous.Span.Start:previous.Span.End]
			if value == "{" || value == ";" {
				return completionStatement
			}
		}
	}

	lineStart := currentLineStart(text, offset)
	if strings.TrimSpace(text[lineStart:offset]) == "" {
		return completionStatement
	}

	return completionExpression
}

func utf8LastRune(text string) (rune, int) {
	return utf8.DecodeLastRuneInString(text)
}

func namespacePrefixAt(text string, offset int) string {
	if offset > len(text) {
		offset = len(text)
	}

	start := offset
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && !strings.ContainsRune(runtime.NamespaceSeparator, r) {
			break
		}

		start -= size
	}

	fragment := text[start:offset]
	separator := strings.LastIndex(fragment, runtime.NamespaceSeparator)
	if separator < 0 {
		return ""
	}

	return fragment[:separator]
}

func currentLineStart(text string, offset int) int {
	start := offset

	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if r == '\r' || r == '\n' || r == '\u2028' || r == '\u2029' {
			return start
		}

		start -= size
	}

	return 0
}

func sortedCompletionItems(items map[string]CompletionItem) []CompletionItem {
	result := make([]CompletionItem, 0, len(items))

	for _, item := range items {
		result = append(result, item)
	}

	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(result[i].Label)
		right := strings.ToLower(result[j].Label)

		if left != right {
			return left < right
		}

		if result[i].Label != result[j].Label {
			return result[i].Label < result[j].Label
		}

		return result[i].Detail < result[j].Detail
	})

	return result
}
