package exec

import "strings"

const defaultOutputContentType = "application/json"

type (
	// RuntimeOptions contains per-run Ferret settings shared by ordinary and
	// debugger execution.
	RuntimeOptions struct {
		OutputContentType string
	}

	// ExecutionOptions is the ordinary Execution name for RuntimeOptions.
	ExecutionOptions = RuntimeOptions
)

func (o RuntimeOptions) normalized() RuntimeOptions {
	o.OutputContentType = strings.TrimSpace(o.OutputContentType)
	if o.OutputContentType == "" {
		o.OutputContentType = defaultOutputContentType
	}

	return o
}
