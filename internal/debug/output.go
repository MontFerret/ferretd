package debug

import (
	"bytes"

	"github.com/MontFerret/api"
)

func cloneContent(content *api.Content) *api.Content {
	if content == nil {
		return nil
	}

	result := *content
	result.Data = bytes.Clone(content.Data)

	return &result
}
