package client

import daemonv1 "github.com/MontFerret/ferretd/gen/ferretd/daemon/v1"

func fromProtoRuntimeCatalog(response *daemonv1.ListRuntimesResponse) (RuntimeCatalog, error) {
	if response == nil || response.DefaultRuntime == "" || len(response.Runtimes) == 0 {
		return RuntimeCatalog{}, errIncompleteRuntimeCatalog
	}

	result := RuntimeCatalog{
		DefaultRuntime: response.DefaultRuntime,
		Runtimes:       make([]RuntimeDescriptor, 0, len(response.Runtimes)),
	}
	seen := make(map[string]struct{}, len(response.Runtimes))
	for _, descriptor := range response.Runtimes {
		if descriptor == nil || descriptor.Name == "" || descriptor.Label == "" || descriptor.Provider == "" {
			return RuntimeCatalog{}, errIncompleteRuntimeCatalog
		}

		if _, duplicate := seen[descriptor.Name]; duplicate {
			return RuntimeCatalog{}, errIncompleteRuntimeCatalog
		}

		seen[descriptor.Name] = struct{}{}
		result.Runtimes = append(result.Runtimes, RuntimeDescriptor{
			Name: descriptor.Name, Label: descriptor.Label, Provider: descriptor.Provider,
		})
	}

	if _, exists := seen[result.DefaultRuntime]; !exists {
		return RuntimeCatalog{}, errIncompleteRuntimeCatalog
	}

	return result, nil
}
