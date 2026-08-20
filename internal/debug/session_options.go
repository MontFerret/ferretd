package debug

import "strings"

const defaultOutputContentType = "application/json"

// SessionOptions contains invocation-specific debugger settings.
type SessionOptions struct {
	OutputContentType string
}

func (o SessionOptions) normalized() SessionOptions {
	o.OutputContentType = strings.TrimSpace(o.OutputContentType)
	if o.OutputContentType == "" {
		o.OutputContentType = defaultOutputContentType
	}

	return o
}
