package source

import (
	"testing"

	apisource "github.com/MontFerret/api/source"
)

func TestMapperOffsetToPosition(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		offset int
		want   Position
	}{
		{name: "ascii", text: "RETURN 1", offset: 7, want: Position{Character: 7}},
		{name: "multiline", text: "LET x = 1\nRETURN x", offset: 10, want: Position{Line: 1}},
		{name: "crlf", text: "LET x = 1\r\nRETURN x", offset: 11, want: Position{Line: 1}},
		{name: "accented boundary", text: "éx", offset: 2, want: Position{Character: 1}},
		{name: "accented interior clamp", text: "éx", offset: 1, want: Position{}},
		{name: "astral boundary", text: "😀x", offset: 4, want: Position{Character: 2}},
		{name: "astral interior clamp", text: "😀x", offset: 2, want: Position{}},
		{name: "negative clamp", text: "abc", offset: -1, want: Position{}},
		{name: "upper clamp", text: "abc", offset: 9, want: Position{Character: 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewMapper(tt.text).OffsetToPosition(tt.offset); got != tt.want {
				t.Fatalf("OffsetToPosition(%d) = %#v, want %#v", tt.offset, got, tt.want)
			}
		})
	}
}

func TestMapperSpanToRange(t *testing.T) {
	mapper := NewMapper("😀a\nbé")

	got := mapper.SpanToRange(apisource.Span{Start: 4, End: 9})
	want := Range{
		Start: Position{Character: 2},
		End:   Position{Line: 1, Character: 2},
	}
	if got != want {
		t.Fatalf("SpanToRange = %#v, want %#v", got, want)
	}

	got = mapper.SpanToRange(apisource.Span{Start: 99, End: -1})
	want = Range{
		Start: Position{Line: 1, Character: 2},
		End:   Position{Line: 1, Character: 2},
	}
	if got != want {
		t.Fatalf("clamped SpanToRange = %#v, want %#v", got, want)
	}
}

func TestMapperPositionToOffset(t *testing.T) {
	mapper := NewMapper("😀a\r\nbé\u2028last")
	tests := []struct {
		position Position
		want     int
	}{
		{position: Position{}, want: 0},
		{position: Position{Character: 1}, want: 0},
		{position: Position{Character: 2}, want: 4},
		{position: Position{Character: 99}, want: 5},
		{position: Position{Line: 1, Character: 1}, want: 8},
		{position: Position{Line: 2}, want: 13},
		{position: Position{Line: 99}, want: len(mapper.Text())},
		{position: Position{Line: ^uint32(0)}, want: len(mapper.Text())},
	}

	for _, tt := range tests {
		if got := mapper.PositionToOffset(tt.position); got != tt.want {
			t.Errorf("PositionToOffset(%#v) = %d, want %d", tt.position, got, tt.want)
		}
	}
}

func TestMapperRoundTripSafeBoundaries(t *testing.T) {
	mapper := NewMapper("é😀\r\nvalue")
	for _, offset := range []int{0, 2, 6, 8, 13} {
		position := mapper.OffsetToPosition(offset)
		if got := mapper.PositionToOffset(position); got != offset {
			t.Errorf("offset %d round trip = %d through %#v", offset, got, position)
		}
	}
}

func TestMapperRequiresReceiver(t *testing.T) {
	var mapper *Mapper
	tests := []struct {
		name string
		call func()
	}{
		{name: "OffsetToPosition", call: func() { _ = mapper.OffsetToPosition(0) }},
		{name: "PositionToOffset", call: func() { _ = mapper.PositionToOffset(Position{}) }},
		{name: "SpanToRange", call: func() { _ = mapper.SpanToRange(apisource.Span{}) }},
		{name: "RangeToSpan", call: func() { _ = mapper.RangeToSpan(Range{}) }},
		{name: "Text", call: func() { _ = mapper.Text() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("call did not panic")
				}
			}()

			tt.call()
		})
	}
}
