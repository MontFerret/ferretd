package language

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/formatter"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

	"github.com/MontFerret/ferretd/internal/source"
)

// DefaultTabSize is the canonical formatting width used when callers omit one.
const DefaultTabSize uint32 = 4

// Format formats the current document using Ferret's canonical formatter.
func (s *Service) Format(ctx context.Context, uri string, tabSize uint32) (*FormattingResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	snapshot, err := s.resolveSnapshot(ctx, uri)
	if err != nil {
		return nil, err
	}

	if tabSize == 0 {
		tabSize = DefaultTabSize
	}

	var output bytes.Buffer
	instance := formatter.New(formatter.WithTabWidth(uint64(tabSize)))
	err = instance.Format(&output, ferretsource.New(snapshot.path, snapshot.text))
	if err != nil {
		if formatterSourceError(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("format document: %w", err)
	}

	formatted := output.String()
	if formatted == snapshot.text {
		return nil, nil
	}

	mapper := source.NewMapper(snapshot.text)
	return &FormattingResult{
		Range: source.Range{
			Start: source.Position{},
			End:   mapper.OffsetToPosition(len(snapshot.text)),
		},
		Text: formatted,
	}, nil
}

func formatterSourceError(err error) bool {
	var diagnostic *diagnostics.Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.Kind != diagnostics.UnexpectedError
	}

	var set *diagnostics.DiagnosticSet
	if errors.As(err, &set) {
		for _, item := range set.Errors() {
			if item.Kind == diagnostics.UnexpectedError {
				return false
			}
		}

		return true
	}

	return false
}
