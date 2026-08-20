package language

import "github.com/MontFerret/ferretd/internal/source"

type (
	// Document is a versioned editor-overlay snapshot.
	Document struct {
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
