package language

import "errors"

var (
	errNilWorkspaceManager = errors.New("language: nil workspace manager")
	errNilFunctions        = errors.New("language: nil functions")

	// ErrDocumentNotOpen indicates that an operation requires an open document.
	ErrDocumentNotOpen = errors.New("document is not open")
	// ErrStaleDocumentVersion indicates that a change does not advance a document version.
	ErrStaleDocumentVersion = errors.New("document version is stale")
	// ErrNoTextChanges indicates that a change notification contained no content.
	ErrNoTextChanges = errors.New("document change contains no text changes")
)
