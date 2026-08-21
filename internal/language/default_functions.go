package language

import (
	"fmt"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/stdlib"
)

// NewDefaultFunctions builds the function metadata for Ferret's full standard library.
func NewDefaultFunctions() (*runtime.Functions, error) {
	library := runtime.NewLibrary()
	if err := stdlib.Full().Register(library); err != nil {
		return nil, fmt.Errorf("register Ferret standard library: %w", err)
	}

	functions, err := library.Build()
	if err != nil {
		return nil, fmt.Errorf("build Ferret standard library: %w", err)
	}

	return functions, nil
}
