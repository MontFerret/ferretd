package language

import (
	"fmt"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
	ferretstdlib "github.com/MontFerret/ferret/v2/pkg/stdlib"

	embeddedstdlib "github.com/MontFerret/ferretd/internal/language/stdlib"
)

// NewDefaultFunctionCatalog builds language metadata for Ferret's full
// standard library. Mismatches degrade metadata but do not prevent startup.
func NewDefaultFunctionCatalog() (*FunctionCatalog, []CatalogWarning, error) {
	library := runtime.NewLibrary()
	if err := ferretstdlib.Full().Register(library); err != nil {
		return nil, nil, fmt.Errorf("register Ferret standard library: %w", err)
	}

	functions, err := library.Build()
	if err != nil {
		return nil, nil, fmt.Errorf("build Ferret standard library: %w", err)
	}

	catalog, err := NewRuntimeFunctionCatalog(functions)
	if err != nil {
		return nil, nil, err
	}

	warnings := catalog.mergeReference(embeddedstdlib.Reference())

	return catalog, warnings, nil
}
