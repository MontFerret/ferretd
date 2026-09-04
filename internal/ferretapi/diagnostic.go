package ferretapi

import (
	"errors"
	"iter"

	"github.com/MontFerret/api"
	apidiagnostics "github.com/MontFerret/api/diagnostics"
	apisource "github.com/MontFerret/api/source"
	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	"github.com/MontFerret/ferret/v2/pkg/vm"
)

type runtimeErrorSet interface {
	Errors() iter.Seq2[int, *vm.RuntimeError]
}

func wrapDiagnosticError(source api.Source, err error) error {
	if err == nil {
		return nil
	}

	values := extractDiagnostics(err)
	if len(values) == 0 {
		return err
	}

	diagnostics := make(apidiagnostics.Diagnostics, len(values))
	for index := range values {
		diagnostics[index] = convertDiagnostic(source, values[index])
	}

	return newDiagnosticError(err, diagnostics)
}

func extractDiagnostics(err error) []*ferretdiagnostics.Diagnostic {
	var runtimeSet runtimeErrorSet
	if errors.As(err, &runtimeSet) {
		var result []*ferretdiagnostics.Diagnostic
		for _, item := range runtimeSet.Errors() {
			if item != nil && item.Diagnostic != nil {
				result = append(result, item.Diagnostic)
			}
		}

		return result
	}

	var runtimeError *vm.RuntimeError
	if errors.As(err, &runtimeError) && runtimeError != nil && runtimeError.Diagnostic != nil {
		return []*ferretdiagnostics.Diagnostic{runtimeError.Diagnostic}
	}

	var set *ferretdiagnostics.DiagnosticSet
	if errors.As(err, &set) && set != nil {
		result := make([]*ferretdiagnostics.Diagnostic, 0, set.Size())
		for _, item := range set.Errors() {
			result = append(result, item)
		}

		return result
	}

	var single *ferretdiagnostics.Diagnostic
	if errors.As(err, &single) && single != nil {
		return []*ferretdiagnostics.Diagnostic{single}
	}

	return nil
}

func convertDiagnostic(source api.Source, value *ferretdiagnostics.Diagnostic) apidiagnostics.Diagnostic {
	result := apidiagnostics.Diagnostic{
		Source: source,
	}
	if value == nil {
		result.Kind = apidiagnostics.UnexpectedError

		return result
	}

	result.Kind = apidiagnostics.Kind(value.Kind.String())
	result.Message = value.Message
	result.Hint = value.Hint
	result.Note = value.Note
	nativeSource := ferretsource.New(source.Name, source.Content)
	result.Annotations = make([]apidiagnostics.Annotation, len(value.Spans))
	for index := range value.Spans {
		span := value.Spans[index]
		position := nativeSource.PositionAt(span.Span)
		result.Annotations[index] = apidiagnostics.Annotation{
			Range: apisource.Range{
				Location: apisource.Location{
					Position:   apisource.Position{Line: position.Line, Column: position.Column},
					SourceName: source.Name,
				},
				Span: apisource.Span{Start: span.Span.Start, End: span.Span.End},
			},
			Message: span.Label,
			Primary: span.Main,
		}
	}

	return result
}
