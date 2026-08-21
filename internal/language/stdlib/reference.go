// Package stdlib owns ferretd's embedded Ferret Standard Library API Reference.
package stdlib

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/MontFerret/specs/pkg/api"
)

var (
	//go:embed api.json
	referenceData []byte

	referenceOnce sync.Once
	reference     *api.Reference
)

// Reference returns the immutable embedded Standard Library API Reference.
// Invalid checked-in data is a build defect and causes a panic on first use.
func Reference() *api.Reference {
	referenceOnce.Do(func() {
		reference = mustParse(referenceData)
	})

	return cloneReference(reference)
}

func mustParse(data []byte) *api.Reference {
	parsed, err := api.Parse(data)
	if err != nil {
		panic(fmt.Sprintf("stdlib: parse embedded API Reference: %v", err))
	}

	return parsed
}
