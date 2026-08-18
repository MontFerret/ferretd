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

	if _, ok := message.(*protocol.InitializeRequest); !ok {
		return message, initializeClientOptions{}, nil
	}

	var raw struct {
		Arguments initializeClientOptions `json:"arguments"`
	}

	if err := json.Unmarshal(content, &raw); err != nil {
		return nil, initializeClientOptions{}, fmt.Errorf("decode initialize client options: %w", err)
	}

	return message, raw.Arguments, nil
}
