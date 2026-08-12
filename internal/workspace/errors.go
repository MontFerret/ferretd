package workspace

import "errors"

var (
	// ErrInvalidRoot reports a workspace root that cannot identify a directory.
	ErrInvalidRoot = errors.New("invalid workspace root")
	// ErrLoad reports a workspace that could not be loaded coherently.
	ErrLoad = errors.New("workspace load failed")
	// ErrNotFound reports an unknown workspace ID.
	ErrNotFound = errors.New("workspace not found")
)
