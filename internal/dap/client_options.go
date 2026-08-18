package dap

import (
	"errors"

	protocol "github.com/google/go-dap"
)

func normalizeClientOptions(arguments protocol.InitializeRequestArguments) (clientOptions, error) {
	pathFormat := arguments.PathFormat
	if pathFormat == "" {
		pathFormat = "path"
	}

	if pathFormat != "path" && pathFormat != "uri" {
		return clientOptions{}, errors.New("pathFormat must be path or uri")
	}

	return clientOptions{
		pathFormat:      pathFormat,
		linesStartAt1:   arguments.LinesStartAt1,
		columnsStartAt1: arguments.ColumnsStartAt1,
	}, nil
}
