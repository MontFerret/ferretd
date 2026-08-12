package workspace

import "io/fs"

type readErrorFS struct {
	fs.FS
	path string
	err  error
}

func (f readErrorFS) ReadFile(name string) ([]byte, error) {
	if name == f.path {
		return nil, f.err
	}

	return fs.ReadFile(f.FS, name)
}
