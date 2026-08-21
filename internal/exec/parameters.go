package exec

import ferretruntime "github.com/MontFerret/ferret/v2/pkg/runtime"

// Parameters is the caller-facing parameter set retained by normal and debug
// execution. Clone recursively isolates map[string]any and []any containers;
// all other values retain their existing Go semantics.
type Parameters map[string]any

// Clone returns an independent copy of the recursively mutable containers.
func (p Parameters) Clone() Parameters {
	if p == nil {
		return nil
	}

	result := make(Parameters, len(p))
	for key, value := range p {
		result[key] = p.cloneValue(value)
	}

	return result
}

func (p Parameters) prepare() (ferretruntime.Params, Parameters, error) {
	retained := p.Clone()
	converted, err := ferretruntime.NewParamsFrom(map[string]any(retained))
	if err != nil {
		return nil, nil, err
	}

	return converted, retained, nil
}

func (p Parameters) cloneValue(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = p.cloneValue(typed[index])
		}

		return result
	case map[string]any:
		return map[string]any(Parameters(typed).Clone())
	default:
		return typed
	}
}
