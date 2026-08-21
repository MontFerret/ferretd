package lsp

import (
	"path/filepath"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/MontFerret/ferretd/internal/source"
)

func initializationRoots(params *protocol.InitializeParams) ([]string, error) {
	if params == nil {
		return nil, nil
	}

	var values []string

	if len(params.WorkspaceFolders) > 0 {
		for _, folder := range params.WorkspaceFolders {
			path, err := source.URI(folder.URI).Path()
			if err != nil {
				return nil, err
			}

			values = append(values, path)
		}
	} else if params.RootURI != nil && *params.RootURI != "" {
		path, err := source.URI(*params.RootURI).Path()
		if err != nil {
			return nil, err
		}

		values = append(values, path)
	} else if params.RootPath != nil && *params.RootPath != "" {
		values = append(values, *params.RootPath)
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return nil, err
		}

		root := filepath.Clean(absolute)
		if _, ok := seen[root]; ok {
			continue
		}

		seen[root] = struct{}{}
		result = append(result, root)
	}

	return result, nil
}
