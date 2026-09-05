package language

import (
	"context"

	apisource "github.com/MontFerret/api/source"

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

	return document, document.resolve(position), nil
}

func (d analyzedDocument) resolve(position source.Position) resolution {
	offset := d.mapper.PositionToOffset(position)
	result := resolution{Offset: offset}

	if symbol, ok := d.analysis.SymbolAt(offset); ok {
		result.Symbol = &symbol
		if symbol.HasDeclaration {
			declaration := d.mapper.SpanToRange(apisource.Span(symbol.SelectionSpan))
			result.Range = declaration
			result.Declaration = &declaration
		}
	}

	if reference, ok := d.analysis.ReferenceAt(offset); ok {
		result.Range = d.mapper.SpanToRange(apisource.Span(reference.Span))
	}

	if call, ok := d.analysis.CallAt(offset); ok {
		result.Call = &call
		if offset >= call.CalleeSpan.Start && offset < call.CalleeSpan.End {
			result.Range = d.mapper.SpanToRange(apisource.Span(call.CalleeSpan))
		}
	}

	if fact, ok := d.analysis.TypeAt(offset); ok {
		result.Type = &fact
		if result.Range == (source.Range{}) {
			result.Range = d.mapper.SpanToRange(apisource.Span(fact.Span))
		}
	}

	return result
}
