package language

import "github.com/MontFerret/ferretd/internal/source"

type (
	overlay struct {
		URI        source.URI
		Path       string
		Version    int32
		Text       string
		generation uint64
	}

	// TextChange replaces the complete text of an open document.
	TextChange struct {
		Text string
	}
)
