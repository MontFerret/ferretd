package language

import (
	"sort"
	"strings"

	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

type registeredFunction struct {
	name     string
	arities  []int
	variadic bool
}

func (s *Service) registeredFunctions() []registeredFunction {
	byIdentity := make(map[string]*registeredFunction)

	for _, name := range s.functions.List() {
		identity := runtime.NormalizeRegisteredName(name)
		entry := byIdentity[identity]

		if entry == nil {
			entry = &registeredFunction{name: name}
			byIdentity[identity] = entry
		}

		if s.functions.A0().Has(name) {
			entry.arities = append(entry.arities, 0)
		}

		if s.functions.A1().Has(name) {
			entry.arities = append(entry.arities, 1)
		}

		if s.functions.A2().Has(name) {
			entry.arities = append(entry.arities, 2)
		}

		if s.functions.A3().Has(name) {
			entry.arities = append(entry.arities, 3)
		}

		if s.functions.A4().Has(name) {
			entry.arities = append(entry.arities, 4)
		}

		entry.variadic = s.functions.Var().Has(name)
	}

	result := make([]registeredFunction, 0, len(byIdentity))

	for _, function := range byIdentity {
		sort.Ints(function.arities)
		result = append(result, *function)
	}

	sort.Slice(result, func(i, j int) bool {
		return runtime.NormalizeRegisteredName(result[i].name) < runtime.NormalizeRegisteredName(result[j].name)
	})

	return result
}

func (s *Service) registeredFunction(name string) (registeredFunction, bool) {
	identity := runtime.NormalizeRegisteredName(name)

	for _, function := range s.registeredFunctions() {
		if runtime.NormalizeRegisteredName(function.name) == identity {
			return function, true
		}
	}

	return registeredFunction{}, false
}

func terminalName(name string) string {
	if index := strings.LastIndex(name, runtime.NamespaceSeparator); index >= 0 {
		return name[index+len(runtime.NamespaceSeparator):]
	}

	return name
}
