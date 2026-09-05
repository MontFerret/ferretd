package ferretapi

import (
	"fmt"

	"github.com/MontFerret/api"
)

type planOptions struct {
	optimizationLevel *api.OptimizationLevel
}

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
