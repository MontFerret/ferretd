package language

import "github.com/MontFerret/ferret/v2/pkg/runtime"

type (
	// CatalogWarningKind classifies degraded function metadata.
	CatalogWarningKind uint8

	// CatalogWarning describes one runtime/API Reference inconsistency.
	CatalogWarning struct {
		Kind CatalogWarningKind
		Name string
	}

	// FunctionCatalog is an immutable normalized index of executable functions.
	FunctionCatalog struct {
		ordered []functionSymbol
		byName  map[string]int
	}

	functionSymbol struct {
		name            string
		identity        string
		arities         []int
		runtimeVariadic bool
		authored        bool
		signatures      []Signature
		detail          string
		documentation   string
		deprecated      bool
	}
)

const (
	// CatalogWarningReferenceOnly identifies API metadata without a runtime function.
	CatalogWarningReferenceOnly CatalogWarningKind = iota + 1
	// CatalogWarningRuntimeOnly identifies a runtime function using fallback metadata.
	CatalogWarningRuntimeOnly
)

// NewRuntimeFunctionCatalog builds fallback metadata from an executable runtime registry.
func NewRuntimeFunctionCatalog(functions *runtime.Functions) (*FunctionCatalog, error) {
	if functions == nil {
		return nil, errNilFunctionCatalogSource
	}

	names := functions.List()
	result := &FunctionCatalog{
		ordered: make([]functionSymbol, 0, len(names)),
		byName:  make(map[string]int, len(names)),
	}

	for _, name := range names {
		identity := runtime.NormalizeRegisteredName(name)
		if _, ok := result.byName[identity]; ok {
			continue
		}

		entry := newRuntimeFunction(functions, name, identity)
		result.byName[identity] = len(result.ordered)
		result.ordered = append(result.ordered, entry)
	}

	return result, nil
}

func newRuntimeFunction(functions *runtime.Functions, name, identity string) functionSymbol {
	result := functionSymbol{name: name, identity: identity}
	if functions.A0().Has(name) {
		result.arities = append(result.arities, 0)
	}

	if functions.A1().Has(name) {
		result.arities = append(result.arities, 1)
	}

	if functions.A2().Has(name) {
		result.arities = append(result.arities, 2)
	}

	if functions.A3().Has(name) {
		result.arities = append(result.arities, 3)
	}

	if functions.A4().Has(name) {
		result.arities = append(result.arities, 4)
	}

	result.runtimeVariadic = functions.Var().Has(name)
	result.cacheCompletion()

	return result
}

func (c *FunctionCatalog) lookup(name string) (functionSymbol, bool) {
	identity := runtime.NormalizeRegisteredName(name)
	index, ok := c.byName[identity]
	if !ok {
		return functionSymbol{}, false
	}

	return c.ordered[index], true
}

func (f functionSymbol) renderedSignatures(variadicArity int) []Signature {
	if f.authored {
		return cloneSignatures(f.signatures)
	}

	result := make([]Signature, 0, len(f.arities)+1)
	for _, arity := range f.arities {
		parameters := placeholderParameters(arity, false)
		result = append(result, Signature{
			Label:      signatureLabel(f.name, parameters),
			Parameters: parameters,
		})
	}

	if f.runtimeVariadic {
		parameters := placeholderParameters(variadicArity, true)
		result = append(result, Signature{
			Label:      signatureLabel(f.name, parameters),
			Parameters: parameters,
			Variadic:   true,
		})
	}

	return result
}

func (f *functionSymbol) cacheCompletion() {
	if !f.authored {
		f.detail = "registered function"
		f.documentation = ""
		f.deprecated = false

		return
	}

	signatures := f.signatures
	f.detail = completionSignatureDetail(signatures)
	f.documentation = RenderSignaturesMarkdown(signatures)
	f.deprecated = len(signatures) > 0
	for _, signature := range signatures {
		if signature.Deprecated == "" {
			f.deprecated = false

			break
		}
	}
}
