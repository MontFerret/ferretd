package language

import (
	"context"
	"sort"

	"github.com/MontFerret/ferretd/internal/source"
)

// SemanticTokens returns full-document syntax tokens overlaid with compiler identity.
func (s *Service) SemanticTokens(ctx context.Context, uri source.URI) ([]SemanticToken, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return nil, err
	}

	semantic := buildSemanticSpans(document.analysis, document.snapshot.text)
	spans := make([]semanticSpan, 0, len(document.analysis.SyntaxTokens())+len(semantic))

	for _, token := range document.analysis.SyntaxTokens() {
		kind, ok := semanticKindForSyntax(token.Kind)
		if !ok || overlapsAny(token.Span, semantic) {
			continue
		}

		spans = append(spans, semanticSpan{span: token.Span, kind: kind, priority: semanticPrioritySyntax})
	}

	spans = append(spans, semantic...)

	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].span.Start != spans[j].span.Start {
			return spans[i].span.Start < spans[j].span.Start
		}

		if spans[i].priority != spans[j].priority {
			return spans[i].priority > spans[j].priority
		}

		return spans[i].span.End < spans[j].span.End
	})

	result := make([]SemanticToken, 0, len(spans))
	lastEnd := -1

	for _, item := range spans {
		if item.span.Start < lastEnd {
			continue
		}

		pieces := splitSemanticSpan(document.mapper, document.snapshot.text, item.span)

		for _, piece := range pieces {
			piece.Kind = item.kind
			piece.Modifiers = item.modifiers
			result = append(result, piece)
		}

		lastEnd = item.span.End
	}

	return result, nil
}
