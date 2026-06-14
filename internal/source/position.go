package source

import "unicode/utf16"

// Position identifies a zero-based position within a source document.
type Position struct {
	Line      uint32
	Character uint32
}

// Range identifies a half-open range within a source document.
type Range struct {
	Start Position
	End   Position
}

// Span identifies a half-open range using rune offsets.
//
// Ferret compiler spans originate from ANTLR's rune-indexed input stream.
type Span struct {
	Start int
	End   int
}

// Mapper converts rune-indexed source offsets into protocol-neutral positions.
type Mapper struct {
	runes []rune
}

// NewMapper creates a source position mapper for text.
func NewMapper(text string) *Mapper {
	return &Mapper{runes: []rune(text)}
}

// OffsetToPosition converts a rune offset to a zero-based UTF-16 position.
func (m *Mapper) OffsetToPosition(offset int) Position {
	if m == nil {
		return Position{}
	}

	offset = clamp(offset, 0, len(m.runes))

	var position Position
	for i := 0; i < offset; i++ {
		switch m.runes[i] {
		case '\n':
			position.Line++
			position.Character = 0
		case '\r':
			if i+1 >= len(m.runes) || m.runes[i+1] != '\n' {
				position.Line++
				position.Character = 0
			}
		default:
			position.Character += uint32(len(utf16.Encode([]rune{m.runes[i]})))
		}
	}

	return position
}

// SpanToRange converts a half-open rune span to a zero-based UTF-16 range.
func (m *Mapper) SpanToRange(span Span) Range {
	if m == nil {
		return Range{}
	}

	start := clamp(span.Start, 0, len(m.runes))
	end := clamp(span.End, start, len(m.runes))

	return Range{
		Start: m.OffsetToPosition(start),
		End:   m.OffsetToPosition(end),
	}
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
