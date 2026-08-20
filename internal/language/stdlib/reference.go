package stdlib

import (
	"fmt"
	"sort"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/specs/pkg/api"
)

const referenceID = "montferret/core"

// Reference is an immutable lookup view over one Ferret Standard Library API Reference.
type Reference struct {
	version   string
	functions map[string]Function
}

// Parse strictly parses one Ferret Standard Library API Reference.
func Parse(data []byte) (*Reference, error) {
	reference, err := api.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse embedded standard library API reference: %w", err)
	}

	if reference.ID != referenceID {
		return nil, fmt.Errorf("standard library API reference id is %q, want %q", reference.ID, referenceID)
	}

	result := &Reference{
		version:   reference.Version,
		functions: make(map[string]Function),
	}

	for _, namespace := range reference.Namespaces {
		for _, function := range namespace.Functions {
			name := function.Name
			if namespace.Name != "" {
				name = namespace.Name + runtime.NamespaceSeparator + function.Name
			}

			identity := runtime.NormalizeRegisteredName(name)
			if _, exists := result.functions[identity]; exists {
				return nil, fmt.Errorf("standard library API reference function %q has duplicate normalized identity %q", name, identity)
			}

			entry := Function{
				Name:       name,
				Namespace:  namespace.Name,
				Signatures: make([]Signature, 0, len(function.Signatures)),
			}

			for _, signature := range function.Signatures {
				converted := Signature{
					Parameters:  make([]Parameter, 0, len(signature.Parameters)),
					Variadic:    signature.Variadic,
					Description: signature.Description,
					Throws:      make([]Throw, 0, len(signature.Throws)),
					Deprecated:  signature.Deprecated,
				}

				for _, parameter := range signature.Parameters {
					converted.Parameters = append(converted.Parameters, Parameter{
						Name:        parameter.Name,
						Type:        parameter.Type,
						Description: parameter.Description,
					})
				}

				if signature.Return != nil {
					converted.Return = &Return{
						Type:        signature.Return.Type,
						Description: signature.Return.Description,
					}
				}

				for _, thrown := range signature.Throws {
					converted.Throws = append(converted.Throws, Throw{
						Error:       thrown.Error,
						Description: thrown.Description,
					})
				}

				entry.Signatures = append(entry.Signatures, converted)
			}

			result.functions[identity] = entry
		}
	}

	return result, nil
}

// Version returns the Ferret version described by the reference.
func (r *Reference) Version() string {
	if r == nil {
		return ""
	}

	return r.version
}

// Lookup returns a defensive copy of metadata for a case-insensitive qualified name.
func (r *Reference) Lookup(name string) (Function, bool) {
	if r == nil {
		return Function{}, false
	}

	function, ok := r.functions[runtime.NormalizeRegisteredName(name)]
	if !ok {
		return Function{}, false
	}

	return cloneFunction(function), true
}

// Functions returns defensive copies of all functions in canonical name order.
func (r *Reference) Functions() []Function {
	if r == nil {
		return []Function{}
	}

	result := make([]Function, 0, len(r.functions))
	for _, function := range r.functions {
		result = append(result, cloneFunction(function))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

func cloneFunction(function Function) Function {
	result := function
	result.Signatures = make([]Signature, len(function.Signatures))

	for index, signature := range function.Signatures {
		result.Signatures[index] = signature
		result.Signatures[index].Parameters = append([]Parameter(nil), signature.Parameters...)
		result.Signatures[index].Throws = append([]Throw(nil), signature.Throws...)

		if signature.Return != nil {
			value := *signature.Return
			result.Signatures[index].Return = &value
		}
	}

	return result
}
