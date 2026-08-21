package language

func cloneSignatures(values []Signature) []Signature {
	result := make([]Signature, len(values))
	for index, value := range values {
		if value.Parameters != nil {
			value.Parameters = append([]SignatureParameter{}, value.Parameters...)
		}

		if value.Throws != nil {
			value.Throws = append([]SignatureThrow{}, value.Throws...)
		}
		if value.Return != nil {
			returnValue := *value.Return
			value.Return = &returnValue
		}

		result[index] = value
	}

	return result
}
