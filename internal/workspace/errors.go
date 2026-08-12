package workspace

import "errors"

var (
	// ErrInvalidRoot reports a workspace root that cannot identify a directory.
	ErrInvalidRoot = errors.New("invalid workspace root")
	// ErrNotFound reports an unknown workspace ID.
	ErrNotFound = errors.New("workspace not found")
)
