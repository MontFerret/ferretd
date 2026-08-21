package language

import "strings"

// RenderSignaturesMarkdown renders callable overloads for completion and hover.
func RenderSignaturesMarkdown(values []Signature) string {
	sections := make([]string, 0, len(values))
	for _, value := range values {
		sections = append(sections, renderSignatureMarkdown(value, true))
	}

	return strings.Join(sections, "\n\n---\n\n")
}

// RenderSignatureDocumentation renders one callable's authored documentation.
func RenderSignatureDocumentation(value Signature) string {
	return renderSignatureMarkdown(value, false)
}

func renderSignatureMarkdown(value Signature, includeLabel bool) string {
	sections := make([]string, 0, 6)
	if includeLabel {
		sections = append(sections, "```fql\n"+value.Label+"\n```")
	}

	if value.Description != "" {
		sections = append(sections, "### Description\n\n"+value.Description)
	}

	parameters := renderSignatureParameters(value.Parameters)
	if parameters != "" {
		sections = append(sections, "### Parameters\n\n"+parameters)
	}

	if value.Return != nil {
		result := "`" + value.Return.Type + "`"
		if value.Return.Description != "" {
			result += " — " + value.Return.Description
		}

		sections = append(sections, "### Returns\n\n"+result)
	}

	throws := renderSignatureThrows(value.Throws)
	if throws != "" {
		sections = append(sections, "### Throws\n\n"+throws)
	}

	if value.Deprecated != "" {
		sections = append(sections, "### Deprecated\n\n"+value.Deprecated)
	}

	return strings.Join(sections, "\n\n")
}

func renderSignatureParameters(values []SignatureParameter) string {
	parameters := make([]string, 0, len(values))
	for _, value := range values {
		if value.Type == "" && value.Description == "" {
			continue
		}

		parameter := "- `" + value.Label + "`"
		if value.Description != "" {
			parameter += " — " + value.Description
		}

		parameters = append(parameters, parameter)
	}

	return strings.Join(parameters, "\n")
}

func renderSignatureThrows(values []SignatureThrow) string {
	throws := make([]string, 0, len(values))
	for _, value := range values {
		thrown := "- `" + value.Error + "`"
		if value.Description != "" {
			thrown += " — " + value.Description
		}

		throws = append(throws, thrown)
	}

	return strings.Join(throws, "\n")
}
