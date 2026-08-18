package language

import (
	"strings"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
)

var (
	statementCompletionWords = map[compiler.SyntaxWord]struct{}{
		compiler.SyntaxWordFor:     {},
		compiler.SyntaxWordFunc:    {},
		compiler.SyntaxWordLet:     {},
		compiler.SyntaxWordReturn:  {},
		compiler.SyntaxWordUse:     {},
		compiler.SyntaxWordVar:     {},
		compiler.SyntaxWordWaitfor: {},
		compiler.SyntaxWordWhile:   {},
	}

	languageCompletionItems, statementCompletionItems = buildLanguageCompletionItems()
)

func buildLanguageCompletionItems() ([]CompletionItem, []CompletionItem) {
	words := compiler.SyntaxWords()
	all := make([]CompletionItem, 0, len(words))
	statements := make([]CompletionItem, 0, len(statementCompletionWords))

	for _, word := range words {
		kind, detail, ok := completionKindForSyntaxWord(word.Category)
		if !ok {
			continue
		}

		spelling := strings.ToLower(word.Spelling)
		item := CompletionItem{
			Label:      spelling,
			InsertText: spelling,
			Detail:     detail,
			Kind:       kind,
		}
		all = append(all, item)

		if _, ok := statementCompletionWords[word.Word]; ok {
			statements = append(statements, item)
		}
	}

	return all, statements
}

func completionKindForSyntaxWord(category compiler.SyntaxWordCategory) (CompletionKind, string, bool) {
	switch category {
	case compiler.SyntaxWordCategoryKeyword:
		return CompletionKindKeyword, "keyword", true
	case compiler.SyntaxWordCategoryLiteral:
		return CompletionKindLiteral, "literal", true
	case compiler.SyntaxWordCategoryOperator:
		return CompletionKindOperator, "operator", true
	default:
		return 0, "", false
	}
}
