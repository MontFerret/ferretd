package language

import (
	"context"
	"sort"

	apisource "github.com/MontFerret/api/source"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

type documentSymbolNode struct {
	symbol compiler.Symbol
	value  DocumentSymbol
	parent int
}

// DocumentSymbols returns compiler declarations arranged by their nearest containing UDF.
func (s *Service) DocumentSymbols(ctx context.Context, uri source.URI) ([]DocumentSymbol, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return nil, err
	}

	var nodes []documentSymbolNode
	for _, symbol := range document.analysis.Symbols() {
		if !symbol.HasDeclaration || symbol.Kind == compiler.SymbolKindBindParameter {
			continue
		}

		nodes = append(nodes, documentSymbolNode{
			symbol: symbol,
			value: DocumentSymbol{
				Name:           symbol.Name,
				Range:          document.mapper.SpanToRange(apisource.Span(symbol.DeclarationSpan)),
				SelectionRange: document.mapper.SpanToRange(apisource.Span(symbol.SelectionSpan)),
				Kind:           symbol.Kind,
				Type:           symbol.Type,
			},
			parent: -1,
		})
	}

	for i := range nodes {
		bestWidth := 0
		for candidate := range nodes {
			if i == candidate || nodes[candidate].symbol.Kind != compiler.SymbolKindUDF {
				continue
			}

			container := nodes[candidate].symbol.DeclarationSpan
			selection := nodes[i].symbol.SelectionSpan

			if container.End <= container.Start || selection.End < selection.Start ||
				selection.Start < container.Start || selection.End > container.End {

				continue
			}

			width := container.End - container.Start
			if nodes[i].parent == -1 || width < bestWidth {
				nodes[i].parent = candidate
				bestWidth = width
			}
		}
	}

	children := make(map[int][]int)
	var roots []int
	for i := range nodes {
		if nodes[i].parent < 0 {
			roots = append(roots, i)
		} else {
			children[nodes[i].parent] = append(children[nodes[i].parent], i)
		}
	}

	var build func(int) DocumentSymbol
	build = func(index int) DocumentSymbol {
		value := nodes[index].value
		for _, child := range children[index] {
			value.Children = append(value.Children, build(child))
		}

		return value
	}

	result := make([]DocumentSymbol, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root))
	}

	return result, nil
}

// Definition returns the compiler declaration for the symbol at position.
func (s *Service) Definition(
	ctx context.Context,
	uri source.URI,
	position source.Position,
) (*Location, error) {
	_, resolved, err := s.resolveAt(ctx, uri, position)
	if err != nil {
		return nil, err
	}

	if resolved.Symbol == nil || resolved.Declaration == nil {
		return nil, nil
	}

	return &Location{
		URI:   uri,
		Range: *resolved.Declaration,
	}, nil
}

// References returns document-local locations for the compiler symbol at position.
func (s *Service) References(
	ctx context.Context,
	uri source.URI,
	position source.Position,
	includeDeclaration bool,
) ([]Location, error) {
	document, resolved, err := s.resolveAt(ctx, uri, position)
	if err != nil {
		return nil, err
	}

	if resolved.Symbol == nil {
		return []Location{}, nil
	}
	symbol := *resolved.Symbol

	locations := make([]Location, 0)
	if includeDeclaration && symbol.HasDeclaration {
		locations = append(locations, Location{
			URI:   uri,
			Range: document.mapper.SpanToRange(apisource.Span(symbol.SelectionSpan)),
		})
	}

	for _, reference := range document.analysis.ReferencesTo(symbol.ID) {
		locations = append(locations, Location{
			URI:   uri,
			Range: document.mapper.SpanToRange(apisource.Span(reference.Span)),
		})
	}

	sort.SliceStable(locations, func(i, j int) bool {
		left := document.mapper.PositionToOffset(locations[i].Range.Start)
		right := document.mapper.PositionToOffset(locations[j].Range.Start)

		return left < right
	})

	return locations, nil
}
