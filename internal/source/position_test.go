package source

import "testing"

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
		{name: "accented", text: "éx", offset: 1, want: Position{Character: 1}},
		{name: "astral", text: "😀x", offset: 1, want: Position{Character: 2}},
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

	got := mapper.SpanToRange(Span{Start: 1, End: 5})
	want := Range{
		Start: Position{Character: 2},
		End:   Position{Line: 1, Character: 2},
	}
	if got != want {
		t.Fatalf("SpanToRange = %#v, want %#v", got, want)
	}

	got = mapper.SpanToRange(Span{Start: 99, End: -1})
	want = Range{
		Start: Position{Line: 1, Character: 2},
		End:   Position{Line: 1, Character: 2},
	}
	if got != want {
		t.Fatalf("clamped SpanToRange = %#v, want %#v", got, want)
	}
}
