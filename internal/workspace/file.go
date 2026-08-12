package workspace

import "github.com/MontFerret/ferretd/internal/source"

// File describes a discovered Ferret source file in the workspace filesystem.
type File struct {
	RelativePath string
	Path         string
	URI          source.URI
}
