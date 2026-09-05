package ferretapi

import (
	"fmt"

	"github.com/MontFerret/api"
	"github.com/MontFerret/ferret/v2"
)

type (
	planOptions struct {
		optimizationLevel *api.OptimizationLevel
	}

	sessionOptions struct {
		options []ferret.SessionOption
	}
)

func newPlanOptions(options []api.PlanOption) (*planOptions, error) {
	result := &planOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (o *planOptions) validate(debug bool) error {
	// Native engines fix normal optimization at construction and expose no
	// per-plan override or configuration query. Only debug's None is guaranteed.
	if o.optimizationLevel != nil && (!debug || *o.optimizationLevel != api.OptimizationNone) {
		return fmt.Errorf(
			"optimization level %d is not supported by the provisional native runtime adapter",
			*o.optimizationLevel,
		)
	}

	return nil
}

func (o *planOptions) SetOptimizationLevel(level api.OptimizationLevel) error {
	o.optimizationLevel = &level

	return nil
}

func newSessionOptions(options []api.SessionOption) (*sessionOptions, error) {
	result := &sessionOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}

		if err := option(result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func (o *sessionOptions) SetParam(name string, value any) error {
	o.options = append(o.options, ferret.WithSessionParam(name, value))

	return nil
}

func (o *sessionOptions) SetParams(parameters map[string]any) error {
	o.options = append(o.options, ferret.WithSessionParams(parameters))

	return nil
}

func (o *sessionOptions) SetOutputContentType(contentType string) error {
	o.options = append(o.options, ferret.WithOutputContentType(contentType))

	return nil
}

func (o *sessionOptions) SetFSRoot(root string) error {
	o.options = append(o.options, ferret.WithSessionFSRoot(root))

	return nil
}
