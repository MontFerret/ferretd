package dap

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	apisource "github.com/MontFerret/api/source"
)

type (
	// sourceCoordinates retains the compiled bytes and immutable decoder boundaries.
	// It deliberately does not use the LSP mapper, which clamps invalid positions
	// and recognizes line separators that native Ferret does not.
	sourceCoordinates struct {
		text       string
		lines      []sourceCoordinateLine
		lineBase   int
		columnBase int
	}

	sourceCoordinateLine struct {
		start      int
		end        int
		boundaries []sourceColumnBoundary
	}

	sourceColumnBoundary struct {
		bytes int
		units int
	}

	clientSourcePosition struct {
		line   int
		column *int
	}
)

func newSourceCoordinates(text string, client clientOptions) *sourceCoordinates {
	c := &sourceCoordinates{text: text}

	if client.linesStartAt1 {
		c.lineBase = 1
	}

	if client.columnsStartAt1 {
		c.columnBase = 1
	}

	for start := 0; ; {
		next := strings.IndexByte(text[start:], '\n')
		end := len(text)

		if next >= 0 {
			end = start + next
		}

		line := sourceCoordinateLine{start: start, end: end, boundaries: []sourceColumnBoundary{{}}}
		if next >= 0 && end > start && text[end-1] == '\r' {
			line.end--
		}

		units := 0
		for offset := start; offset < line.end; {
			// Like ANTLR's []rune input and Ferret's byte-span normalization,
			// malformed UTF-8 consumes one original byte per replacement rune.
			r, size := utf8.DecodeRuneInString(text[offset:line.end])
			offset += size
			units += utf16.RuneLen(r)
			line.boundaries = append(line.boundaries, sourceColumnBoundary{bytes: offset - start, units: units})
		}

		c.lines = append(c.lines, line)

		if next < 0 {
			break
		}

		start = end + 1
	}

	return c
}

func (c *sourceCoordinates) fromClient(line int, column *int) (apisource.Position, error) {
	if line < c.lineBase || line-c.lineBase >= len(c.lines) {
		return apisource.Position{}, fmt.Errorf("%w: line is outside the compiled source", errInvalidSourcePosition)
	}

	index := line - c.lineBase
	position := apisource.Position{Line: index + 1}

	if column == nil {
		return position, nil
	}

	if *column < c.columnBase {
		return apisource.Position{}, fmt.Errorf("%w: column precedes the line", errInvalidSourcePosition)
	}

	units := *column - c.columnBase
	boundaries := c.lines[index].boundaries

	boundary := sort.Search(len(boundaries), func(i int) bool { return boundaries[i].units >= units })
	if boundary == len(boundaries) || boundaries[boundary].units != units {
		return apisource.Position{}, fmt.Errorf("%w: column is not a UTF-16 boundary in the compiled line", errInvalidSourcePosition)
	}

	position.Column = boundaries[boundary].bytes + 1

	return position, nil
}

func (c *sourceCoordinates) toClient(position apisource.Position) (clientSourcePosition, error) {
	if position.Line < 1 || position.Line > len(c.lines) {
		return clientSourcePosition{}, fmt.Errorf("%w: native line is outside the compiled source", errInvalidSourcePosition)
	}

	if position.Column < 0 {
		return clientSourcePosition{}, fmt.Errorf("%w: native column precedes the line", errInvalidSourcePosition)
	}

	result := clientSourcePosition{line: position.Line - 1 + c.lineBase}
	// Native breakpoint requests use zero to preserve a line-only request.
	if position.Column == 0 {
		return result, nil
	}

	bytes := position.Column - 1
	boundaries := c.lines[position.Line-1].boundaries

	boundary := sort.Search(len(boundaries), func(i int) bool { return boundaries[i].bytes >= bytes })
	if boundary == len(boundaries) || boundaries[boundary].bytes != bytes {
		return clientSourcePosition{}, fmt.Errorf("%w: native column is not a byte boundary in the compiled line", errInvalidSourcePosition)
	}

	column := boundaries[boundary].units + c.columnBase
	result.column = &column

	return result, nil
}
