package source

import ferretsource "github.com/MontFerret/ferret/v2/pkg/source"

// Span identifies a half-open range using UTF-8 byte offsets.
type Span struct {
	Start int
	End   int
}

// SpanFromFerret converts a Ferret source span without changing its byte offsets.
func SpanFromFerret(value ferretsource.Span) Span {
	return Span{Start: value.Start, End: value.End}
}
