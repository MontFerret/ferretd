package dap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	protocol "github.com/google/go-dap"
)

type (
	// go-dap uses an int for the optional column. Retain its raw presence so
	// a zero-based first column cannot be mistaken for a line-only breakpoint.
	sourceBreakpointsRequest struct {
		protocol.SetBreakpointsRequest
		columns []json.RawMessage
	}

	// Pointer coordinates preserve real zero-based positions on the wire.
	sourceBreakpoint struct {
		protocol.Breakpoint
		Line   *int `json:"line,omitempty"`
		Column *int `json:"column,omitempty"`
	}

	sourceBreakpointsResponse struct {
		protocol.Response
		Body struct {
			Breakpoints []sourceBreakpoint `json:"breakpoints"`
		} `json:"body"`
	}
)

func (r *sourceBreakpointsRequest) column(index int) (*int, error) {
	if index >= len(r.columns) || len(r.columns[index]) == 0 {
		return nil, nil
	}

	raw := r.columns[index]
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("breakpoint column must be an integer when supplied")
	}

	var column int
	if err := json.Unmarshal(raw, &column); err != nil {
		return nil, fmt.Errorf("invalid breakpoint column: %w", err)
	}

	return &column, nil
}
