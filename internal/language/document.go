package language

type (
	// Document is a versioned editor-overlay snapshot.
	Document struct {
		URI        string
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
