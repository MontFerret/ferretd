package language

import (
	"context"

	"github.com/MontFerret/ferret/v2/pkg/compiler"

	"github.com/MontFerret/ferretd/internal/source"
)

// Resolution combines the compiler facts available at one source position.
type Resolution struct {
	Symbol      *compiler.Symbol
	Reference   *compiler.Reference
	Call        *compiler.Call
	Type        *compiler.TypeFact
	Range       source.Range
	Declaration *source.Range
	Offset      int
	Snapshot    SnapshotID
}

// ResolveAt resolves compiler identity, reference, call, and type facts at position.
func (s *Service) ResolveAt(ctx context.Context, uri string, position source.Position) (Resolution, error) {
	_, resolved, err := s.resolveAt(ctx, uri, position)

	return resolved, err
}

func (s *Service) resolveAt(
	ctx context.Context,
	uri string,
	position source.Position,
) (analyzedDocument, Resolution, error) {
	document, err := s.analyzedDocument(ctx, uri)
	if err != nil {
		return analyzedDocument{}, Resolution{}, err
	}

	return document, resolveAnalyzed(document, position), nil
}
