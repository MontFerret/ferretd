package exec

import "strings"

const defaultOutputContentType = "application/json"

// ExecutionOptions contains invocation-specific Ferret settings.
type ExecutionOptions struct {
	OutputContentType string
}

func (o ExecutionOptions) normalized() ExecutionOptions {
	o.OutputContentType = strings.TrimSpace(o.OutputContentType)
	if o.OutputContentType == "" {
		o.OutputContentType = defaultOutputContentType
	}

	return o
}
