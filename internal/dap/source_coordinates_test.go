package dap

import (
	"errors"
	"math"
	"testing"

	apisource "github.com/MontFerret/api/source"
)

func TestSourceCoordinatesRoundTripDecoderBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		bytes []int
		units []int
	}{
		{"empty", "", []int{0}, []int{0}},
		{"ascii", "abc", []int{0, 1, 2, 3}, []int{0, 1, 2, 3}},
		{"two byte", "é", []int{0, 2}, []int{0, 1}},
		{"three byte", "中", []int{0, 3}, []int{0, 1}},
		{"supplementary", "😀a", []int{0, 4, 5}, []int{0, 2, 3}},
		{"several supplementary", "😀😀", []int{0, 4, 8}, []int{0, 2, 4}},
		{"combining", "e\u0301", []int{0, 1, 3}, []int{0, 1, 2}},
		{"tab", "\té", []int{0, 1, 3}, []int{0, 1, 2}},
		{"standalone carriage return", "\ré", []int{0, 1, 3}, []int{0, 1, 2}},
		{"unicode separators", "\u2028\u2029", []int{0, 3, 6}, []int{0, 1, 2}},
		{"malformed bytes", "\xff\xf0\x9f", []int{0, 1, 2, 3}, []int{0, 1, 2, 3}},
		{"mixed malformed", "é\x80😀\xf0\x9f", []int{0, 2, 3, 7, 8, 9}, []int{0, 1, 2, 4, 5, 6}},
		{"encoded replacement", "\ufffd", []int{0, 3}, []int{0, 1}},
		{"invalid surrogate encoding", "\xed\xa0\x80", []int{0, 1, 2, 3}, []int{0, 1, 2, 3}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, lineBase := range []int{0, 1} {
				for _, columnBase := range []int{0, 1} {
					for _, newline := range []string{"\n", "\r\n"} {
						text := "😀" + newline + test.text + newline
						c := newSourceCoordinates(text, clientOptions{linesStartAt1: lineBase == 1, columnsStartAt1: columnBase == 1})
						for i, offset := range test.bytes {
							native := apisource.Position{Line: 2, Column: offset + 1}

							client, err := c.toClient(native)
							if err != nil || client.line != 1+lineBase || client.column == nil || *client.column != test.units[i]+columnBase {
								t.Fatalf("native %+v: client %+v, error %v", native, client, err)
							}

							roundTrip, err := c.fromClient(client.line, client.column)
							if err != nil || roundTrip != native {
								t.Fatalf("native round trip = %+v, error %v, want %+v", roundTrip, err, native)
							}

							column := test.units[i] + columnBase

							decoded, err := c.fromClient(1+lineBase, &column)
							if err != nil || decoded != native {
								t.Fatalf("client (%d,%d): native %+v, error %v", 1+lineBase, column, decoded, err)
							}

							again, err := c.toClient(decoded)
							if err != nil || again.line != 1+lineBase || again.column == nil || *again.column != column {
								t.Fatalf("client round trip = %+v, error %v", again, err)
							}
						}

						// Both an omitted column and the final empty line survive base translation.
						lineOnly, err := c.fromClient(2+lineBase, nil)
						if err != nil || lineOnly != (apisource.Position{Line: 3}) {
							t.Fatalf("line only = %+v, error %v", lineOnly, err)
						}

						echoed, err := c.toClient(lineOnly)
						if err != nil || echoed.line != 2+lineBase || echoed.column != nil {
							t.Fatalf("line-only response = %+v, error %v", echoed, err)
						}

						column := columnBase
						if end, err := c.fromClient(2+lineBase, &column); err != nil || end != (apisource.Position{Line: 3, Column: 1}) {
							t.Fatalf("final empty line = %+v, error %v", end, err)
						}
					}
				}
			}
		})
	}
}

func TestSourceCoordinatesRejectInvalidBoundaries(t *testing.T) {
	for _, lineBase := range []int{0, 1} {
		for _, columnBase := range []int{0, 1} {
			c := newSourceCoordinates("é😀\r\n", clientOptions{linesStartAt1: lineBase == 1, columnsStartAt1: columnBase == 1})
			for _, line := range []int{-1, lineBase - 1, 2 + lineBase, math.MaxInt} {
				if _, err := c.fromClient(line, nil); !errors.Is(err, errInvalidSourcePosition) {
					t.Fatalf("line %d: %v", line, err)
				}
			}

			for _, column := range []int{-1, columnBase - 1, 2 + columnBase, 4 + columnBase, math.MaxInt} {
				if _, err := c.fromClient(lineBase, &column); !errors.Is(err, errInvalidSourcePosition) {
					t.Fatalf("column %d: %v", column, err)
				}
			}

			for _, position := range []apisource.Position{
				{Line: 0}, {Line: -1}, {Line: 3}, {Line: math.MaxInt},
				{Line: 1, Column: -1}, {Line: 1, Column: 2},
				{Line: 1, Column: 4}, {Line: 1, Column: 5}, {Line: 1, Column: 6},
				{Line: 1, Column: 8}, {Line: 1, Column: math.MaxInt},
				{Line: 2, Column: 2},
			} {
				if _, err := c.toClient(position); !errors.Is(err, errInvalidSourcePosition) {
					t.Fatalf("native %+v: %v", position, err)
				}
			}
		}
	}
}

func TestSourceCoordinatesLogicalLineEnds(t *testing.T) {
	for _, test := range []struct {
		text   string
		native apisource.Position
		column int
	}{
		{"", apisource.Position{Line: 1, Column: 1}, 1},
		{"a", apisource.Position{Line: 1, Column: 2}, 2},
		{"\n", apisource.Position{Line: 2, Column: 1}, 1},
		{"\r\n", apisource.Position{Line: 2, Column: 1}, 1},
		{"😀\r\n", apisource.Position{Line: 1, Column: 5}, 3},
		{"\r", apisource.Position{Line: 1, Column: 2}, 2},
		{"😀\r", apisource.Position{Line: 1, Column: 6}, 4},
	} {
		c := newSourceCoordinates(test.text, clientOptions{linesStartAt1: true, columnsStartAt1: true})

		got, err := c.toClient(test.native)
		if err != nil || got.line != test.native.Line || got.column == nil || *got.column != test.column {
			t.Fatalf("%q: native %+v -> %+v, error %v", test.text, test.native, got, err)
		}

		native, err := c.fromClient(got.line, got.column)
		if err != nil || native != test.native {
			t.Fatalf("%q: round trip %+v, error %v", test.text, native, err)
		}
	}
}
