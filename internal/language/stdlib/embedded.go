package stdlib

import (
	_ "embed"
	"fmt"
	"sync"
)

var (
	//go:embed api.json
	embeddedData []byte

	defaultOnce      sync.Once
	defaultReference *Reference
)

// Default returns the embedded Ferret Standard Library API Reference.
func Default() *Reference {
	defaultOnce.Do(func() {
		reference, err := Parse(embeddedData)
		if err != nil {
			panic(fmt.Errorf("load embedded Ferret Standard Library API Reference: %w", err))
		}

		defaultReference = reference
	})

	return defaultReference
}
