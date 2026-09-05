package debug

import "github.com/MontFerret/api"

func cloneOutput(output *api.Output) *api.Output {
	if output == nil {
		return nil
	}

	return &api.Output{
		ContentType: output.ContentType,
		Content:     append([]byte(nil), output.Content...),
	}
}
