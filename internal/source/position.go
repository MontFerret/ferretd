package source

import (
	"sort"
	"unicode/utf8"
)

type (
	// Position identifies a zero-based UTF-16 position within a source document.
	Position struct {
		Line      uint32
		Character uint32
	}

	// Range identifies a half-open range within a source document.
	Range struct {
		Start Position
		End   Position
	}

	// Span identifies a half-open range using UTF-8 byte offsets.
	Span struct {
		Start int
		End   int
	}

	// Mapper converts between UTF-8 byte offsets and zero-based UTF-16 positions.
	Mapper struct {
		text       string
		lineStarts []int
		lineEnds   []int
	}
)

// NewMapper creates a source position mapper for text.
func NewMapper(text string) *Mapper {
	mapper := &Mapper{text: text, lineStarts: []int{0}}

	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if size == 0 {
			break
		}

		switch r {
		case '\r':
			mapper.lineEnds = append(mapper.lineEnds, offset)
			offset += size

			if offset < len(text) && text[offset] == '\n' {
				offset++
			}

			mapper.lineStarts = append(mapper.lineStarts, offset)
		case '\n', '\u2028', '\u2029':
			mapper.lineEnds = append(mapper.lineEnds, offset)
			offset += size
			mapper.lineStarts = append(mapper.lineStarts, offset)
		default:
			offset += size
		}
	}

	mapper.lineEnds = append(mapper.lineEnds, len(text))

	return mapper
}

// OffsetToPosition converts a UTF-8 byte offset to a zero-based UTF-16 position.
func (m *Mapper) OffsetToPosition(offset int) Position {
	if m == nil {
		return Position{}
	}

	offset = m.clampOffset(offset)
	line := sort.Search(len(m.lineStarts), func(i int) bool {
		return m.lineStarts[i] > offset
	}) - 1

	if line < 0 {
		line = 0
	}

	lineOffset := offset
	if lineOffset > m.lineEnds[line] {
		lineOffset = m.lineEnds[line]
	}

	return Position{
		Line:      uint32(line),
		Character: utf16Width(m.text[m.lineStarts[line]:lineOffset]),
	}
}

// PositionToOffset converts a zero-based UTF-16 position to a UTF-8 byte offset.
func (m *Mapper) PositionToOffset(position Position) int {
	if m == nil || len(m.lineStarts) == 0 {
		return 0
	}

	if uint64(position.Line) >= uint64(len(m.lineStarts)) {
		return len(m.text)
	}
	line := int(position.Line)

	start := m.lineStarts[line]
	end := m.lineEnds[line]
	want := position.Character
	var character uint32

	for offset := start; offset < end; {
		r, size := utf8.DecodeRuneInString(m.text[offset:end])
		width := uint32(1)

		if r > 0xffff {
			width = 2
		}

		if want <= character || want < character+width {
			return offset
		}

		character += width
		offset += size
	}

	return end
}

// SpanToRange converts a half-open UTF-8 byte span to a UTF-16 range.
func (m *Mapper) SpanToRange(span Span) Range {
	if m == nil {
		return Range{}
	}

	start := m.clampOffset(span.Start)
	end := m.clampOffset(span.End)

	if end < start {
		end = start
	}

	return Range{Start: m.OffsetToPosition(start), End: m.OffsetToPosition(end)}
}

// RangeToSpan converts a UTF-16 range to a half-open UTF-8 byte span.
func (m *Mapper) RangeToSpan(value Range) Span {
	if m == nil {
		return Span{}
	}

	start := m.PositionToOffset(value.Start)
	end := m.PositionToOffset(value.End)

	if end < start {
		end = start
	}

	return Span{Start: start, End: end}
}

// Text returns the mapper's immutable source text.
func (m *Mapper) Text() string {
	if m == nil {
		return ""
	}

	return m.text
}

func (m *Mapper) clampOffset(offset int) int {
	offset = clamp(offset, 0, len(m.text))

	for offset > 0 && offset < len(m.text) && !utf8.RuneStart(m.text[offset]) {
		offset--
	}

	return offset
}
