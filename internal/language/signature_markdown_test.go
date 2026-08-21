package language

import (
	"strings"
	"testing"
)

func TestRenderSignaturesMarkdownUsesSharedAuthoredSections(t *testing.T) {
	signature := Signature{
		Label:       "old(value: String)",
		Description: "Old function.",
		Parameters: []SignatureParameter{{
			Name:        "value",
			Label:       "value: String",
			Type:        "String",
			Description: "Input value.",
		}},
		Return:     &SignatureReturn{Type: "Object", Description: "Result value."},
		Throws:     []SignatureThrow{{Error: "TypeError", Description: "Input is invalid."}},
		Deprecated: "Use replacement.",
	}

	markdown := RenderSignaturesMarkdown([]Signature{signature})
	fragments := []string{
		"```fql\nold(value: String)\n```",
		"### Description\n\nOld function.",
		"### Parameters\n\n- `value: String` — Input value.",
		"### Returns\n\n`Object` — Result value.",
		"### Throws\n\n- `TypeError` — Input is invalid.",
		"### Deprecated\n\nUse replacement.",
	}
	previous := -1
	for _, fragment := range fragments {
		index := strings.Index(markdown, fragment)
		if index <= previous {
			t.Fatalf("Markdown = %q, section %q is missing or out of order", markdown, fragment)
		}
		previous = index
	}

	documentation := RenderSignatureDocumentation(signature)
	if strings.Contains(documentation, "```fql") || !strings.Contains(documentation, "### Deprecated") {
		t.Fatalf("signature documentation = %q", documentation)
	}

	mixed := RenderSignaturesMarkdown([]Signature{signature, {Label: "old(value: Array)"}})
	if strings.Count(mixed, "### Deprecated") != 1 {
		t.Fatalf("mixed deprecation Markdown = %q", mixed)
	}
}
