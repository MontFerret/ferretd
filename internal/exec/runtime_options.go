package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultOutputContentType = "application/json"

// RuntimeOptions contains per-run Universal runtime settings shared by ordinary and
// debugger execution. WorkingDirectorySet distinguishes an absent filesystem
// root override from an explicitly supplied value.
type RuntimeOptions struct {
	OutputContentType   string
	WorkingDirectory    string
	WorkingDirectorySet bool
}

func (o RuntimeOptions) normalized() (RuntimeOptions, error) {
	o.OutputContentType = strings.TrimSpace(o.OutputContentType)
	if o.OutputContentType == "" {
		o.OutputContentType = defaultOutputContentType
	}

	if !o.WorkingDirectorySet {
		o.WorkingDirectory = ""

		return o, nil
	}

	workingDirectory := strings.TrimSpace(o.WorkingDirectory)
	if workingDirectory == "" {
		return RuntimeOptions{}, fmt.Errorf("%w: working directory must not be blank", ErrInvalidExecutionOptions)
	}

	if !filepath.IsAbs(workingDirectory) {
		return RuntimeOptions{}, fmt.Errorf("%w: working directory must be absolute", ErrInvalidExecutionOptions)
	}

	canonical, err := filepath.EvalSymlinks(filepath.Clean(workingDirectory))
	if err != nil {
		return RuntimeOptions{}, fmt.Errorf("%w: resolve working directory: %w", ErrInvalidExecutionOptions, err)
	}

	canonical = filepath.Clean(canonical)
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return RuntimeOptions{}, fmt.Errorf("%w: open working directory: %w", ErrInvalidExecutionOptions, err)
	}

	if err := root.Close(); err != nil {
		return RuntimeOptions{}, fmt.Errorf("%w: close working directory validation handle: %w", ErrInvalidExecutionOptions, err)
	}

	o.WorkingDirectory = canonical

	return o, nil
}
