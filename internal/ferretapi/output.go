package ferretapi

import "github.com/MontFerret/api"

func convertOutput(output *api.Output) api.Output {
	if output == nil {
		return api.Output{}
	}

	return api.Output{ContentType: output.ContentType, Content: append([]byte(nil), output.Content...)}
}
