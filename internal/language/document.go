package language

// Document is an open source document snapshot.
type Document struct {
	URI     string
	Path    string
	Version int32
	Text    string
}

// TextChange replaces the complete text of an open document.
type TextChange struct {
	Text string
}
