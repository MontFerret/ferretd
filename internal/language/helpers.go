package language

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MontFerret/ferret/v2/pkg/compiler"
	ferretdiagnostics "github.com/MontFerret/ferret/v2/pkg/diagnostics"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	ferretsource "github.com/MontFerret/ferret/v2/pkg/source"
	diagnosticprojection "github.com/MontFerret/ferretd/internal/diagnostic"
	"github.com/MontFerret/ferretd/internal/source"
)

func fmtDocumentNotOpen(uri string) error {
	return fmt.Errorf("%w: %s", ErrDocumentNotOpen, uri)
}

func convertFerretDiagnostic(uri string, mapper *source.Mapper, diagnostic *ferretdiagnostics.Diagnostic) Diagnostic {
	return diagnosticprojection.Convert(uri, mapper, diagnostic)
}

func toSourceSpan(span ferretsource.Span) source.Span {
	return source.Span{Start: span.Start, End: span.End}
}

func buildSemanticSpans(analysis *compiler.Analysis, text string) []semanticSpan {
	if analysis == nil {
		return nil
	}

	symbols := analysis.Symbols()
	byID := make(map[compiler.SymbolID]compiler.Symbol, len(symbols))
	result := make([]semanticSpan, 0, len(symbols)+len(analysis.References())+len(analysis.Calls()))

	for _, symbol := range symbols {
		byID[symbol.ID] = symbol

		if !symbol.HasDeclaration {
			continue
		}

		modifiers := SemanticTokenDeclaration
		if !symbol.Mutable {
			modifiers |= SemanticTokenReadonly
		}

		result = append(result, semanticSpan{
			span:      symbol.SelectionSpan,
			kind:      semanticKindForSymbol(symbol.Kind),
			modifiers: modifiers,
			priority:  semanticPriorityDeclaration,
		})
	}

	for _, reference := range analysis.References() {
		symbol, ok := byID[reference.Symbol]
		if !ok {
			continue
		}

		var modifiers SemanticTokenModifiers
		if !symbol.Mutable {
			modifiers = SemanticTokenReadonly
		}

		result = append(result, semanticSpan{
			span:      reference.Span,
			kind:      semanticKindForSymbol(symbol.Kind),
			modifiers: modifiers,
			priority:  semanticPriorityReference,
		})
	}

	for _, call := range analysis.Calls() {
		span := call.CalleeSpan
		if span.Start >= 0 && span.End <= len(text) && span.End > span.Start {
			if separator := strings.LastIndex(text[span.Start:span.End], runtime.NamespaceSeparator); separator >= 0 {
				span.Start += separator + len(runtime.NamespaceSeparator)
			}
		}
		result = append(result, semanticSpan{
			span:     span,
			kind:     SemanticTokenFunction,
			priority: semanticPriorityCall,
		})
	}

	return result
}

func semanticKindForSymbol(kind compiler.SymbolKind) SemanticTokenKind {
	switch kind {
	case compiler.SymbolKindUDF:
		return SemanticTokenFunction
	case compiler.SymbolKindNamespaceAlias:
		return SemanticTokenNamespace
	case compiler.SymbolKindFunctionParameter, compiler.SymbolKindBindParameter:
		return SemanticTokenParameter
	default:
		return SemanticTokenVariable
	}
}

func semanticKindForSyntax(kind compiler.SyntaxTokenKind) (SemanticTokenKind, bool) {
	switch kind {
	case compiler.SyntaxTokenKindNamespace:
		return SemanticTokenNamespace, true
	case compiler.SyntaxTokenKindKeyword:
		return SemanticTokenKeyword, true
	case compiler.SyntaxTokenKindString:
		return SemanticTokenString, true
	case compiler.SyntaxTokenKindNumber, compiler.SyntaxTokenKindDuration:
		return SemanticTokenNumber, true
	case compiler.SyntaxTokenKindComment:
		return SemanticTokenComment, true
	case compiler.SyntaxTokenKindOperator:
		return SemanticTokenOperator, true
	default:
		return 0, false
	}
}

func overlapsAny(span ferretsource.Span, values []semanticSpan) bool {
	for _, value := range values {
		if span.Start < value.span.End && value.span.Start < span.End {
			return true
		}
	}

	return false
}

func splitSemanticSpan(mapper *source.Mapper, text string, span ferretsource.Span) []SemanticToken {
	start := span.Start
	end := span.End

	if start < 0 {
		start = 0
	}

	if end > len(text) {
		end = len(text)
	}

	if end <= start {
		return nil
	}

	var result []SemanticToken
	segmentStart := start
	for offset := start; offset < end; {
		r, size := utf8.DecodeRuneInString(text[offset:end])
		if r != '\r' && r != '\n' && r != '\u2028' && r != '\u2029' {
			offset += size

			continue
		}

		if offset > segmentStart {
			result = append(result, SemanticToken{Range: mapper.SpanToRange(source.Span{Start: segmentStart, End: offset})})
		}
		offset += size
		if r == '\r' && offset < end && text[offset] == '\n' {
			offset++
		}
		segmentStart = offset
	}

	if segmentStart < end {
		result = append(result, SemanticToken{Range: mapper.SpanToRange(source.Span{Start: segmentStart, End: end})})
	}

	return result
}

func symbolKindName(kind compiler.SymbolKind) string {
	switch kind {
	case compiler.SymbolKindFunctionParameter:
		return "parameter"
	case compiler.SymbolKindNamespaceAlias:
		return "namespace"
	case compiler.SymbolKindLoopBinding:
		return "loop binding"
	case compiler.SymbolKindMatchBinding:
		return "match binding"
	case compiler.SymbolKindCollectBinding:
		return "collect binding"
	default:
		return "binding"
	}
}

func resolveAnalyzed(document analyzedDocument, position source.Position) Resolution {
	offset := document.mapper.PositionToOffset(position)
	result := Resolution{Offset: offset, Snapshot: document.snapshot.id}

	if symbol, ok := document.analysis.SymbolAt(offset); ok {
		result.Symbol = &symbol
		if symbol.HasDeclaration {
			declaration := document.mapper.SpanToRange(toSourceSpan(symbol.SelectionSpan))
			result.Range = declaration
			result.Declaration = &declaration
		}
	}

	if reference, ok := document.analysis.ReferenceAt(offset); ok {
		result.Reference = &reference
		result.Range = document.mapper.SpanToRange(toSourceSpan(reference.Span))
	}

	if call, ok := document.analysis.CallAt(offset); ok {
		result.Call = &call
		if offset >= call.CalleeSpan.Start && offset < call.CalleeSpan.End {
			result.Range = document.mapper.SpanToRange(toSourceSpan(call.CalleeSpan))
		}
	}

	if fact, ok := document.analysis.TypeAt(offset); ok {
		result.Type = &fact
		if result.Range == (source.Range{}) {
			result.Range = document.mapper.SpanToRange(toSourceSpan(fact.Span))
		}
	}

	return result
}

func spanContainsSpan(container, contained ferretsource.Span) bool {
	return container.End > container.Start && contained.End >= contained.Start &&
		contained.Start >= container.Start && contained.End <= container.End
}
