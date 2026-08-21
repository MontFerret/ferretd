package exec

import "strings"

const defaultOutputContentType = "application/json"

// RuntimeOptions contains per-run Ferret settings shared by ordinary and
// debugger execution.
type RuntimeOptions struct {
	OutputContentType string
}

func (o RuntimeOptions) normalized() RuntimeOptions {
	o.OutputContentType = strings.TrimSpace(o.OutputContentType)
	if o.OutputContentType == "" {
		o.OutputContentType = defaultOutputContentType
	}

	return o
}
