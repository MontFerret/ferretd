package dap

import (
	"bufio"
	"encoding/json"
	"fmt"

	protocol "github.com/google/go-dap"
)

func readProtocolMessage(reader *bufio.Reader) (protocol.Message, initializeClientOptions, error) {
	content, err := protocol.ReadBaseMessage(reader)
	if err != nil {
		return nil, initializeClientOptions{}, err
	}

	message, err := protocol.DecodeProtocolMessage(content)
	if err != nil {
		return nil, initializeClientOptions{}, err
	}

	switch typed := message.(type) {
	case *protocol.InitializeRequest:
		var raw struct {
			Arguments initializeClientOptions `json:"arguments"`
		}

		if err := json.Unmarshal(content, &raw); err != nil {
			return nil, initializeClientOptions{}, fmt.Errorf("decode initialize client options: %w", err)
		}

		return message, raw.Arguments, nil
	case *protocol.SetBreakpointsRequest:
		var raw struct {
			Arguments struct {
				Breakpoints []struct {
					Column json.RawMessage `json:"column"`
				} `json:"breakpoints"`
			} `json:"arguments"`
		}

		if err := json.Unmarshal(content, &raw); err != nil {
			return nil, initializeClientOptions{}, fmt.Errorf("decode breakpoint columns: %w", err)
		}

		request := &sourceBreakpointsRequest{
			SetBreakpointsRequest: *typed,
			columns:               make([]json.RawMessage, len(raw.Arguments.Breakpoints)),
		}
		for i, breakpoint := range raw.Arguments.Breakpoints {
			request.columns[i] = breakpoint.Column
		}

		return request, initializeClientOptions{}, nil
	default:
		return message, initializeClientOptions{}, nil
	}
}
