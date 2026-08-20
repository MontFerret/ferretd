package language

import (
	"sync"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

type (
	registeredFunction struct {
		name       string
		namespace  string
		identity   string
		signatures []Signature
		detail     string
		deprecated bool
	}

	functionIndex struct {
		ordered []registeredFunction
		byName  map[string]int
	}

	functionMetadata struct {
		name       string
		namespace  string
		signatures []Signature
		detail     string
		deprecated bool
	}

	functionMetadataIndex map[string]functionMetadata
)

var (
	defaultMetadataOnce sync.Once
	defaultMetadata     functionMetadataIndex
)

func (i functionIndex) lookup(name string) (registeredFunction, bool) {
	identity := runtime.NormalizeRegisteredName(name)
	index, ok := i.byName[identity]
	if !ok {
		return registeredFunction{}, false
	}

	return i.ordered[index], true
}
