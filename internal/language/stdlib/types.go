package stdlib

type (
	// Function contains authored metadata for one Standard Library function.
	Function struct {
		Name       string
		Namespace  string
		Signatures []Signature
	}

	// Signature contains authored metadata for one callable overload.
	Signature struct {
		Parameters  []Parameter
		Variadic    bool
		Description string
		Return      *Return
		Throws      []Throw
		Deprecated  string
	}

	// Parameter describes one authored function parameter.
	Parameter struct {
		Name        string
		Type        string
		Description string
	}

	// Return describes one authored function result.
	Return struct {
		Type        string
		Description string
	}

	// Throw describes one authored Ferret-visible failure.
	Throw struct {
		Error       string
		Description string
	}
)
