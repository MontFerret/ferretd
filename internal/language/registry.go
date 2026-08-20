package language

import "github.com/MontFerret/ferret/v2/pkg/runtime"

type (
	registeredFunction struct {
		name     string
		identity string
		arities  []int
		variadic bool
	}

	functionIndex struct {
		ordered []registeredFunction
		byName  map[string]int
	}
)

func newFunctionIndex(functions *runtime.Functions) functionIndex {
	names := functions.List()
	result := functionIndex{
		ordered: make([]registeredFunction, 0, len(names)),
		byName:  make(map[string]int, len(names)),
	}

	for _, name := range names {
		identity := runtime.NormalizeRegisteredName(name)
		if _, ok := result.byName[identity]; ok {
			continue
		}

		entry := registeredFunction{name: name, identity: identity}
		if functions.A0().Has(name) {
			entry.arities = append(entry.arities, 0)
		}

		if functions.A1().Has(name) {
			entry.arities = append(entry.arities, 1)
		}

		if functions.A2().Has(name) {
			entry.arities = append(entry.arities, 2)
		}

		if functions.A3().Has(name) {
			entry.arities = append(entry.arities, 3)
		}

		if functions.A4().Has(name) {
			entry.arities = append(entry.arities, 4)
		}

		entry.variadic = functions.Var().Has(name)
		result.byName[identity] = len(result.ordered)
		result.ordered = append(result.ordered, entry)
	}

	return result
}

func (i functionIndex) lookup(name string) (registeredFunction, bool) {
	identity := runtime.NormalizeRegisteredName(name)
	index, ok := i.byName[identity]
	if !ok {
		return registeredFunction{}, false
	}

	return i.ordered[index], true
}
