package workspace

import "errors"

var (
	// ErrInvalidRoot reports a workspace root that cannot identify a directory.
	ErrInvalidRoot = errors.New("invalid workspace root")
	// ErrLoad reports a workspace that could not be loaded coherently.
	ErrLoad = errors.New("workspace load failed")
	// ErrNotFound reports an unknown workspace ID.
	ErrNotFound = errors.New("workspace not found")
	// ErrClosed reports a workspace whose retained runtime is closing or closed.
	ErrClosed = errors.New("workspace closed")
	// ErrDocumentNotFound reports an unknown document path within a workspace.
	ErrDocumentNotFound = errors.New("workspace document not found")
	// ErrDocumentUnavailable reports a discovered document whose contents could not be loaded.
	ErrDocumentUnavailable = errors.New("workspace document unavailable")
)
