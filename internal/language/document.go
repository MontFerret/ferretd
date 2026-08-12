package language

type (
	// Document is an open source document snapshot.
	Document struct {
		URI     string
		Path    string
		Version int32
		Text    string
	}

	// TextChange replaces the complete text of an open document.
	TextChange struct {
		Text string
	}
)
