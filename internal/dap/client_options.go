package dap

import "errors"

type initializeClientOptions struct {
	PathFormat      *string `json:"pathFormat"`
	LinesStartAt1   *bool   `json:"linesStartAt1"`
	ColumnsStartAt1 *bool   `json:"columnsStartAt1"`
}

func normalizeClientOptions(arguments initializeClientOptions) (clientOptions, error) {
	pathFormat := "path"
	if arguments.PathFormat != nil && *arguments.PathFormat != "" {
		pathFormat = *arguments.PathFormat
	}

	if pathFormat != "path" && pathFormat != "uri" {
		return clientOptions{}, errors.New("pathFormat must be path or uri")
	}

	linesStartAt1 := true
	if arguments.LinesStartAt1 != nil {
		linesStartAt1 = *arguments.LinesStartAt1
	}

	columnsStartAt1 := true
	if arguments.ColumnsStartAt1 != nil {
		columnsStartAt1 = *arguments.ColumnsStartAt1
	}

	return clientOptions{
		pathFormat:      pathFormat,
		linesStartAt1:   linesStartAt1,
		columnsStartAt1: columnsStartAt1,
	}, nil
}
