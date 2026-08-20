package language

import (
	"context"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

type resolution struct {
	Symbol      *compiler.Symbol
	Call        *compiler.Call
	Type        *compiler.TypeFact
	Range       source.Range
	Declaration *source.Range
	Offset      int
}

func (s *Service) resolveAt(
	ctx context.Context,
	uri source.URI,
	position source.Position,
) (analyzedDocument, resolution, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return analyzedDocument{}, resolution{}, err
	}

	return document, resolveAnalyzed(document, position), nil
}

func resolveAnalyzed(document analyzedDocument, position source.Position) resolution {
	offset := document.mapper.PositionToOffset(position)
	result := resolution{Offset: offset}

	if symbol, ok := document.analysis.SymbolAt(offset); ok {
		result.Symbol = &symbol
		if symbol.HasDeclaration {
			declaration := document.mapper.SpanToRange(source.SpanFromFerret(symbol.SelectionSpan))
			result.Range = declaration
			result.Declaration = &declaration
		}
	}

	if reference, ok := document.analysis.ReferenceAt(offset); ok {
		result.Range = document.mapper.SpanToRange(source.SpanFromFerret(reference.Span))
	}

	if call, ok := document.analysis.CallAt(offset); ok {
		result.Call = &call
		if offset >= call.CalleeSpan.Start && offset < call.CalleeSpan.End {
			result.Range = document.mapper.SpanToRange(source.SpanFromFerret(call.CalleeSpan))
		}
	}

	if fact, ok := document.analysis.TypeAt(offset); ok {
		result.Type = &fact
		if result.Range == (source.Range{}) {
			result.Range = document.mapper.SpanToRange(source.SpanFromFerret(fact.Span))
		}
	}

	return result
}
