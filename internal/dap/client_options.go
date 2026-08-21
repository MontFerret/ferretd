package dap

import "errors"

type (
	initializeClientOptions struct {
		PathFormat      *string `json:"pathFormat"`
		LinesStartAt1   *bool   `json:"linesStartAt1"`
		ColumnsStartAt1 *bool   `json:"columnsStartAt1"`
	}

	clientOptions struct {
		pathFormat      pathFormat
		linesStartAt1   bool
		columnsStartAt1 bool
	}

	pathFormat string
)

const (
	pathFormatPath pathFormat = "path"
	pathFormatURI  pathFormat = "uri"
)

func (o initializeClientOptions) normalized() (clientOptions, error) {
	format := pathFormatPath
	if o.PathFormat != nil && *o.PathFormat != "" {
		format = pathFormat(*o.PathFormat)
	}

	if format != pathFormatPath && format != pathFormatURI {
		return clientOptions{}, errors.New("pathFormat must be path or uri")
	}

	linesStartAt1 := true
	if o.LinesStartAt1 != nil {
		linesStartAt1 = *o.LinesStartAt1
	}

	columnsStartAt1 := true
	if o.ColumnsStartAt1 != nil {
		columnsStartAt1 = *o.ColumnsStartAt1
	}

	return clientOptions{
		pathFormat:      format,
		linesStartAt1:   linesStartAt1,
		columnsStartAt1: columnsStartAt1,
	}, nil
}
