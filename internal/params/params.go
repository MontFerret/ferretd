// Package params prepares caller-owned parameter values for retained daemon state.
package params

import "github.com/MontFerret/ferret/v2/pkg/runtime"

// Prepare converts values to Ferret parameters and returns the recursively
// copied map retained by the daemon. It preserves Ferret's accepted value set;
// only map[string]any and []any containers receive recursive copy semantics.
func Prepare(values map[string]any) (runtime.Params, map[string]any, error) {
	retained := Clone(values)
	converted, err := runtime.NewParamsFrom(retained)
	if err != nil {
		return nil, nil, err
	}

	return converted, retained, nil
}

// Clone copies a parameter map. Only map[string]any and []any containers are
// copied recursively; all other values retain their existing Go semantics.
func Clone(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = cloneValue(value)
	}

	return result
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = cloneValue(typed[index])
		}

		return result
	case map[string]any:
		return Clone(typed)
	default:
		return typed
	}
}
